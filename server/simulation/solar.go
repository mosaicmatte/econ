package simulation

import (
	"math"
	"os"
	"strconv"
	"time"
)

const (
	// DefaultSiteLat is the default latitude (Ho Chi Minh City, 10.8231° N).
	DefaultSiteLat = 10.8231
	// DefaultSiteLon is the default longitude (Ho Chi Minh City, 106.6297° E).
	DefaultSiteLon = 106.6297
	// SolarConstantWPerM2 is the extraterrestrial solar irradiance constant (W/m²).
	SolarConstantWPerM2 = 1361.0
)

// SiteLat returns the site latitude from WEATHER_LAT or DefaultSiteLat.
func SiteLat() float64 {
	if s := os.Getenv("WEATHER_LAT"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return DefaultSiteLat
}

// SiteLon returns the site longitude from WEATHER_LON or DefaultSiteLon.
func SiteLon() float64 {
	if s := os.Getenv("WEATHER_LON"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return DefaultSiteLon
}

// SolarPosition computes the astronomical solar zenith angle, equation of time,
// solar declination, and clear-sky irradiance components (DNI, GHI) for a given
// timestamp and geographic coordinates using the Spencer (1971) / NOAA algorithm.
//
// Returns:
//   - cosZenith: cosine of solar zenith angle (<= 0 at night)
//   - zenithRad: solar zenith angle in radians [0, pi]
//   - dni: Direct Normal Irradiance in W/m² (0.0 when sun is below horizon)
//   - ghi: Global Horizontal Irradiance in W/m² (0.0 when sun is below horizon)
func SolarPosition(t time.Time, latDeg, lonDeg float64) (cosZenith, zenithRad, dni, ghi float64) {
	utc := t.UTC()
	dayOfYear := float64(utc.YearDay())
	utcHour := float64(utc.Hour()) + float64(utc.Minute())/60.0 + float64(utc.Second())/3600.0 + float64(utc.Nanosecond())/3.6e12

	// 1. Fractional year angle gamma (radians) (Spencer 1971)
	gamma := (2.0 * math.Pi / 365.0) * (dayOfYear - 1.0 + (utcHour-12.0)/24.0)

	// 2. Equation of time (EoT) in minutes
	eot := 229.18 * (0.000075 +
		0.001868*math.Cos(gamma) - 0.032077*math.Sin(gamma) -
		0.014615*math.Cos(2.0*gamma) - 0.040849*math.Sin(2.0*gamma))

	// 3. Solar declination angle delta (radians)
	decl := 0.006918 -
		0.399912*math.Cos(gamma) + 0.070257*math.Sin(gamma) -
		0.006758*math.Cos(2.0*gamma) + 0.000907*math.Sin(2.0*gamma) -
		0.002697*math.Cos(3.0*gamma) + 0.001480*math.Sin(3.0*gamma)

	// 4. True solar time in hours (0..24)
	timeOffsetMin := 4.0*lonDeg + eot
	solarTimeHours := utcHour + timeOffsetMin/60.0
	for solarTimeHours < 0 {
		solarTimeHours += 24.0
	}
	for solarTimeHours >= 24.0 {
		solarTimeHours -= 24.0
	}

	// 5. Hour angle omega (radians): 0 at solar noon (12:00), -pi at midnight (00:00)
	omega := (solarTimeHours - 12.0) * 15.0 * (math.Pi / 180.0)

	// 6. Solar Zenith Angle (theta_z)
	latRad := latDeg * (math.Pi / 180.0)
	cosZenith = math.Sin(latRad)*math.Sin(decl) + math.Cos(latRad)*math.Cos(decl)*math.Cos(omega)
	zenithRad = math.Acos(math.Max(-1.0, math.Min(1.0, cosZenith)))

	// 7. Clear-sky Direct and Global Irradiance (Meinel / ASHRAE clear sky model)
	if cosZenith <= 0.0 {
		// Sun is below horizon (strictly 0.0 W/m² at solar midnight / night)
		return cosZenith, zenithRad, 0.0, 0.0
	}

	// Extraterrestrial solar flux adjusted for Earth orbital eccentricity
	eccCorrection := 1.0 + 0.033*math.Cos(2.0*math.Pi*dayOfYear/365.0)
	i0 := SolarConstantWPerM2 * eccCorrection

	// Direct normal irradiance with atmospheric extinction
	airMass := 1.0 / math.Max(0.01, cosZenith)
	opticalDepth := 0.18 // Tropical sea-level clear atmosphere optical depth
	dni = i0 * math.Exp(-opticalDepth*airMass)

	// Global Horizontal Irradiance: GHI = DNI * cos(zenith) + diffuse horizontal
	const diffuseRatio = 0.10
	ghi = dni*cosZenith + diffuseRatio*dni*cosZenith

	return cosZenith, zenithRad, dni, ghi
}

// ClearSkyGhi returns the clear-sky Global Horizontal Irradiance (GHI) in W/m²
// for the building site coordinates at timestamp t.
func ClearSkyGhi(t time.Time) float64 {
	return ClearSkyGhiAt(t, SiteLat(), SiteLon())
}

// ClearSkyGhiAt returns the clear-sky GHI in W/m² for specific coordinates at time t.
func ClearSkyGhiAt(t time.Time, latDeg, lonDeg float64) float64 {
	_, _, _, ghi := SolarPosition(t, latDeg, lonDeg)
	return ghi
}
