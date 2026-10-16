package focus

import "testing"

func TestPanicAsErrorNil(t *testing.T) {
	t.Parallel()
	if err := panicAsError("AssertTest", nil); err != nil {
		t.Errorf("panicAsError(nil) = %v, want nil", err)
	}
	if err := panicAsError("AssertTest", "boom"); err == nil {
		t.Error("panicAsError(boom) = nil, want an error")
	}
}
