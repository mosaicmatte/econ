package simulation

// Local-first resolution for the data files a deployment owns.
//
// THE PROBLEM THIS SOLVES
//
// Three files under server/data are tracked in git AND rewritten by the running system:
//
//   building-data.json     — replaced wholesale by the blueprint importer (blueprint.go)
//   brick-ontology.json    — replaced alongside it
//   programme-library.json — site calibration is the entire documented purpose of the file
//
// They are tracked because a fresh clone has to boot into a working building. But that
// means the moment a collaborator digitizes their own floorplan or calibrates their own
// site, their working tree diverges from the repository on a file the repository also
// changes — so `git pull` reports a conflict on a 448 KB generated JSON, and every obvious
// way out of that conflict (checkout, reset, stash-drop, "take theirs") DELETES the
// building they just deployed. The learned models were protected from this long ago by
// being gitignored; the building itself never was.
//
// THE FIX
//
// git carries `<name>.json` as the shipped DEFAULT. The runtime reads `<name>.local.json`
// whenever it exists, and every runtime WRITE goes to the local copy only. The local copy
// is gitignored, so:
//
//   * a fresh clone finds no local file, falls back to the tracked default, and runs
//   * a deployment writes a local file that git never sees, and `git pull` cannot touch it
//   * the tracked default keeps evolving upstream without fighting anyone's deployment
//
// This is the same shape as wifi_secrets.example.h -> wifi_secrets.h, applied to the files
// the engine writes rather than the ones a human fills in.

import (
	"os"
	"path/filepath"
	"strings"
)

// The tracked files a deployment may replace. Named here so the read path, the write path
// and the gitignore all refer to the same three strings.
const (
	BuildingDataFile     = "building-data.json"
	BuildingDataHomeFile = "building-data-home.json"
	OntologyFile         = "brick-ontology.json"
	OntologyHomeFile     = "brick-ontology.home.json"
	ProgrammeLibraryFile = "programme-library.json"
)

// ModelFileFor maps a model name/alias ("domestic-home", "home", "house", "multi-level", "office", "tower")
// to its corresponding building JSON file name. Returns empty string if unrecognized.
func ModelFileFor(model string) string {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "home", "domestic-home", "domestic_home", "house", "residential", "bldg-econ-house-hcmc":
		return BuildingDataHomeFile
	case "office", "multi-level", "multilevel", "multi_level", "tower", "commercial", "bldg-econ-digitized":
		return BuildingDataFile
	default:
		return ""
	}
}

// BuildingDataPathFor returns the resolved DataPath for the requested model name.
// If model is empty or unrecognized, it falls back to the active BuildingDataFile.
func BuildingDataPathFor(model string) string {
	if f := ModelFileFor(model); f != "" {
		return DataPath(f)
	}
	return DataPath(BuildingDataFile)
}

// DataDir is where the engine keeps its data files. Overridable so a second engine can be
// run against an isolated copy instead of sharing one directory with a live one — two
// engines writing the same room-dynamics.json corrupts the learned state of both.
func DataDir() string {
	if d := os.Getenv("ECON_DATA_DIR"); d != "" {
		return d
	}
	return "./data"
}

// LocalPath is where a runtime WRITE of `name` must go: the gitignored `.local.json`
// sibling of the tracked default. Writes never target the tracked file.
func LocalPath(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return filepath.Join(DataDir(), base+".local"+filepath.Ext(name))
}

// DefaultPath is the tracked file the repository ships.
func DefaultPath(name string) string {
	return filepath.Join(DataDir(), name)
}

// DataPath is where a runtime READ of `name` should come from: the deployment's own local
// copy when it has one, otherwise the shipped default.
func DataPath(name string) string {
	if p := LocalPath(name); fileExists(p) {
		return p
	}
	return DefaultPath(name)
}

// IsLocal reports whether DataPath(name) resolved to a deployment-owned file rather than
// the tracked default — surfaced at boot so it is obvious which building is running.
func IsLocal(name string) bool { return fileExists(LocalPath(name)) }

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

