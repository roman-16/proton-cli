package account

import "testing"

// The three positions the recording has, and the numbers Proton keeps them in.
// A level and its number have to survive the round trip, since one is what a
// person types and the other is what is written to the account.
func TestSecurityLogLevelsSurviveTheRoundTrip(t *testing.T) {
	for _, c := range []struct {
		state int
		level string
	}{
		{logAuthDisabled, LogOff},
		{logAuthBasic, LogOn},
		{logAuthAdvanced, LogDetailed},
	} {
		if got := logLevel(c.state); got != c.level {
			t.Errorf("logLevel(%d) = %q, want %q", c.state, got, c.level)
		}
		if got := logAuthValue(c.level); got != c.state {
			t.Errorf("logAuthValue(%q) = %d, want %d", c.level, got, c.state)
		}
	}
}

// A number Proton has not used yet is read as off rather than as on: a log that
// might not be recording is the honest reading of one nobody here understands.
func TestSecurityLogReadsAnUnknownStateAsOff(t *testing.T) {
	if got := logLevel(9); got != LogOff {
		t.Errorf("logLevel(9) = %q, want %q", got, LogOff)
	}
}
