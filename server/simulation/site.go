package simulation

// Which network this engine is attached to — and therefore, in practice, where it is.
//
// THE PROBLEM THIS SOLVES
//
// Learned state already records WHICH BUILDING it describes (see baselineDoc) and WHICH
// OCCUPANCY MODEL produced it (see OccupancyModelVersion). Neither answers a third
// question: is the engine currently AT that building?
//
// The fixture travels with the machine. A laptop carrying building-data.local.json to a
// demo, a café, or an office keeps the same buildingId, so every provenance check passes
// and the engine carries on folding a completely different environment into the house's
// learned baselines — different ambient, no edge nodes reporting, a load profile from
// whatever the twin does when nothing is connected. The state files parse, the ids match,
// and the model quietly becomes an average of two places.
//
// WHAT IDENTIFIES A SITE
//
// The default gateway's hardware address. It is the router's own MAC, so it is unique to
// the physical network, it does not change when the machine's DHCP lease does, and it is
// the SAME whether the engine is on Wi-Fi or plugged into ethernet at that site — the ARP
// entry for the gateway resolves to one MAC across both interfaces. That last property is
// why this is keyed on the gateway rather than on the Wi-Fi SSID: an engine on ethernet has
// no SSID at all, macOS returns a redacted one without Location Services permission, and a
// renamed network would look like a different building.
//
// The subnet is mixed in as well, so two sites behind identically-cloned routers still
// separate.
//
// WHAT IS STORED
//
// A hash, never the address itself. The MAC of someone's home router is not a thing to
// write into a state file that may be copied, shared or attached to a bug report, and it is
// not a thing to print in a log line. The fingerprint is opaque and comparison is all
// anyone needs from it.
//
// WHEN IT CANNOT BE DETERMINED
//
// An empty fingerprint means "cannot tell", which is NOT the same as "somewhere else". An
// engine with no route to a gateway, or on a platform this cannot read, must not have its
// learned state destroyed on the strength of an unanswered question — so absence never
// triggers a discard, in either direction. This is the same rule the dashboard follows for
// a null seriesId.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	siteOnce sync.Once
	siteFp   string
)

// SiteFingerprint is an opaque, stable identifier for the network this engine is attached
// to, or "" when it cannot be determined.
//
// SITE_FINGERPRINT overrides detection entirely, and is read on every call rather than
// cached: a deployment behind a router it does not control, or one that deliberately wants
// several machines to share a single site identity, declares it, and a declaration is
// authoritative the moment it is made. Only the network PROBE is cached — it shells out to
// the routing and ARP tables, and the answer does not change while the process runs.
func SiteFingerprint() string {
	if v := strings.TrimSpace(os.Getenv("SITE_FINGERPRINT")); v != "" {
		return hashSite("declared", v)
	}
	siteOnce.Do(func() { siteFp = detectSiteFingerprint() })
	return siteFp
}

// detectSiteFingerprint probes the network. Callers want SiteFingerprint.
func detectSiteFingerprint() string {
	gwIP, err := defaultGateway()
	if err != nil || gwIP == nil {
		return ""
	}
	mac := gatewayHardwareAddr(gwIP)
	subnet := subnetContaining(gwIP)
	if mac == "" && subnet == "" {
		return ""
	}
	// The MAC is the discriminating part; the subnet only separates cloned routers. A
	// site with no resolvable ARP entry still gets a (weaker) fingerprint from its subnet
	// rather than none at all.
	return hashSite(mac, subnet)
}

func hashSite(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	// 16 hex chars is 64 bits: far beyond collision range for the handful of networks one
	// engine will ever see, and short enough to read in a log line.
	return hex.EncodeToString(h[:])[:16]
}

// --- gateway discovery ------------------------------------------------------

func defaultGateway() (net.IP, error) {
	switch runtime.GOOS {
	case "linux":
		return defaultGatewayProc()
	case "darwin", "freebsd", "openbsd", "netbsd":
		return defaultGatewayRouteCmd()
	default:
		return nil, fmt.Errorf("no gateway lookup for %s", runtime.GOOS)
	}
}

// defaultGatewayProc reads /proc/net/route. Fields are hex, little-endian, and the default
// route is the one with a zero destination.
func defaultGatewayProc() (net.IP, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || fields[1] != "00000000" {
			continue
		}
		v, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			continue
		}
		ip := make(net.IP, 4)
		binary.LittleEndian.PutUint32(ip, uint32(v))
		if !ip.Equal(net.IPv4zero) {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("no default route in /proc/net/route")
}

// defaultGatewayRouteCmd parses `route -n get default` on the BSDs. Fixed arguments, no
// interpolation, and a short timeout so a wedged routing table cannot stall boot.
func defaultGatewayRouteCmd() (net.IP, error) {
	out, err := runBounded("route", "-n", "get", "default")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimSuffix(fields[0], ":") == "gateway" {
			if ip := net.ParseIP(fields[1]); ip != nil {
				return ip, nil
			}
		}
	}
	return nil, fmt.Errorf("no gateway in route output")
}

// gatewayHardwareAddr resolves the gateway's MAC from the ARP cache. Empty when the entry
// is missing — the caller falls back to the subnet alone rather than failing outright.
func gatewayHardwareAddr(ip net.IP) string {
	switch runtime.GOOS {
	case "linux":
		return arpProc(ip)
	case "darwin", "freebsd", "openbsd", "netbsd":
		return arpCmd(ip)
	}
	return ""
}

func arpProc(ip net.IP) string {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	want := ip.String()
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 4 && fields[0] == want {
			return normalizeMac(fields[3])
		}
	}
	return ""
}

func arpCmd(ip net.IP) string {
	// `arp -n <ip>` prints e.g. "? (192.168.50.1) at bc:fc:e7:bf:33:54 on en0 ...".
	out, err := runBounded("arp", "-n", ip.String())
	if err != nil {
		return ""
	}
	fields := strings.Fields(out)
	for i, f := range fields {
		if f == "at" && i+1 < len(fields) {
			return normalizeMac(fields[i+1])
		}
	}
	return ""
}

// normalizeMac lower-cases and zero-pads each octet. macOS prints "bc:fc:e7:bf:33:54" but
// abbreviates leading zeros as "0" rather than "00", so the same address can render two
// ways and would otherwise hash to two different sites.
func normalizeMac(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || strings.Contains(s, "incomplete") {
		return ""
	}
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return ""
	}
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
		if len(parts[i]) != 2 {
			return ""
		}
	}
	return strings.Join(parts, ":")
}

// subnetContaining returns the CIDR of the local interface the gateway sits in — the
// network address and prefix, never this machine's own address, which changes with its
// DHCP lease and would make every renewal look like a new site.
func subnetContaining(gw net.IP) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || !ipnet.Contains(gw) {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			if bits == 0 {
				continue
			}
			return fmt.Sprintf("%s/%d", ipnet.IP.Mask(ipnet.Mask).String(), ones)
		}
	}
	return ""
}

func runBounded(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// sameSite compares a stored fingerprint against this engine's own.
//
// Either side being unknown means the question cannot be answered, and an unanswered
// question never destroys learned state: state written before this check existed carries no
// fingerprint, and an engine with no network to probe has none to offer.
// SameSite is sameSite for callers outside this package (the plug savings counter lives in
// package main but is the same kind of state: a measurement of one building in one place).
func SameSite(stored string) bool { return sameSite(stored) }

func sameSite(stored string) bool {
	current := SiteFingerprint()
	if stored == "" || current == "" {
		return true
	}
	return stored == current
}
