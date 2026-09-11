package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestMain doubles as a portable child process for exec tests: when
// AGENTDROP_CLI_TEST_CHILD is set, the test binary prints selected environment
// and its arguments, then exits with AGENTDROP_CLI_TEST_EXIT. This avoids
// depending on sh or cmd.exe.
func TestMain(m *testing.M) {
	if os.Getenv("AGENTDROP_CLI_TEST_CHILD") != "" {
		fmt.Printf("%s|%s|%s", os.Getenv("AGENTDROP_API_TOKEN"), os.Getenv("AGENTDROP_ACCESS_KEY"), strings.Join(os.Args[1:], ","))
		code, _ := strconv.Atoi(os.Getenv("AGENTDROP_CLI_TEST_EXIT"))
		os.Exit(code)
	}
	os.Exit(m.Run())
}
