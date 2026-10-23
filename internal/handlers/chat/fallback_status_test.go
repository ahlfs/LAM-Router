package chat

import "testing"

// Demotion must only follow failures that point at the account. Request-level
// statuses used to demote a healthy connection to the back of the queue, which
// is what pushed priorities to 60+ on this router.
func TestConnectionFaultStatus(t *testing.T) {
	cases := map[int]bool{
		400: false, // malformed request
		404: false, // model not available upstream
		413: false, // payload too large
		422: false, // unprocessable body
		499: false, // client closed request
		401: true,  // account auth
		403: true,  // account permission
		429: true,  // quota / rate limit
		500: true,  // upstream failure
		502: true,
		503: true,
	}
	for code, want := range cases {
		if got := connectionFaultStatus(code); got != want {
			t.Errorf("connectionFaultStatus(%d) = %v, want %v", code, got, want)
		}
	}
}
