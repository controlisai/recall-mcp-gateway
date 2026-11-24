package baseline

// Profile defines default behavioral expectations for a class of MCP servers.
// Applied to new baselines when a server_type is configured, giving anomaly
// detection a starting point from minute one instead of waiting 24h/50 samples.
type Profile struct {
	Name               string  // profile identifier
	MeanCallsPerHour   float64 // expected call rate
	StdDevCallsPerHour float64 // expected deviation (for z-score thresholds)
	MeanErrorRate      float64 // expected error rate (0.0-1.0)
	MeanSignalsPerHour float64 // expected signal detection rate
}

// builtinProfiles contains pre-built baseline profiles for common MCP server types.
var builtinProfiles = map[string]Profile{
	"filesystem": {
		Name:               "filesystem",
		MeanCallsPerHour:   120,
		StdDevCallsPerHour: 60,
		MeanErrorRate:      0.03,
		MeanSignalsPerHour: 1,
	},
	"github": {
		Name:               "github",
		MeanCallsPerHour:   60,
		StdDevCallsPerHour: 30,
		MeanErrorRate:      0.02,
		MeanSignalsPerHour: 0.5,
	},
	"database": {
		Name:               "database",
		MeanCallsPerHour:   150,
		StdDevCallsPerHour: 80,
		MeanErrorRate:      0.03,
		MeanSignalsPerHour: 3, // SQL patterns expected
	},
	"slack": {
		Name:               "slack",
		MeanCallsPerHour:   30,
		StdDevCallsPerHour: 15,
		MeanErrorRate:      0.01,
		MeanSignalsPerHour: 2, // PII in messages
	},
	"web-search": {
		Name:               "web-search",
		MeanCallsPerHour:   50,
		StdDevCallsPerHour: 25,
		MeanErrorRate:      0.04,
		MeanSignalsPerHour: 1,
	},
	"generic": {
		Name:               "generic",
		MeanCallsPerHour:   60,
		StdDevCallsPerHour: 40,
		MeanErrorRate:      0.03,
		MeanSignalsPerHour: 1,
	},
}

// GetProfile returns a pre-built baseline profile by server type name.
// Returns nil if the server type is not recognized.
func GetProfile(serverType string) *Profile {
	if p, ok := builtinProfiles[serverType]; ok {
		return &p
	}
	return nil
}

// ProfileNames returns all available profile names.
func ProfileNames() []string {
	names := make([]string, 0, len(builtinProfiles))
	for k := range builtinProfiles {
		names = append(names, k)
	}
	return names
}
