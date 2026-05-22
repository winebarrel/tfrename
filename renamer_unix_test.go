//go:build !windows

package tfrename

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRename_InPlaceWriteError covers the in-place write-error branch.
// Relies on Unix file permissions, so it's excluded from Windows.
func TestRename_InPlaceWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	tmp := copyInputToTemp(t, "testdata/variable/input")
	path := filepath.Join(tmp, "main.tf")
	require.NoError(t, os.Chmod(path, 0o400))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	target, err := ParseTarget(KindVariable, "region", "aws_region")
	require.NoError(t, err)
	r := NewRenamer(tmp, target)
	err = r.Rename(true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write")
}
