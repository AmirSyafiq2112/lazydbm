package secret

import "testing"

func TestErrNotFound(t *testing.T) {
	if ErrNotFound == nil || ErrNotFound.Error() == "" {
		t.Fatal("ErrNotFound should be a real error")
	}
}
