package provider

import (
	"testing"
)

func TestNewRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	if _, err := New(t.Context(), "gitlab"); err == nil {
		t.Error("expected an error for an unsupported provider")
	}
}
