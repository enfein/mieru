//go:build linux

package tun

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const capNetAdmin = 12

func checkTunPrivileges() error {
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		return fmt.Errorf("TUN device /dev/net/tun is not available: %w", err)
	}

	f, err := os.Open("/proc/self/status")
	if err != nil {
		return fmt.Errorf("failed to read /proc/self/status: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "CapEff:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			break
		}
		mask, err := strconv.ParseUint(fields[1], 16, 64)
		if err != nil {
			return fmt.Errorf("failed to parse CapEff %q: %w", fields[1], err)
		}
		if !capNetAdminSet(mask) {
			return fmt.Errorf("CAP_NET_ADMIN capability is required to create a TUN interface, but it is missing. Run: sudo setcap cap_net_admin+ep %s", os.Args[0])
		}
		return nil
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read /proc/self/status: %w", err)
	}
	return fmt.Errorf("CAP_NET_ADMIN capability is required to create a TUN interface, but it is missing. Run: sudo setcap cap_net_admin+ep %s", os.Args[0])
}

func capNetAdminSet(mask uint64) bool {
	return mask&(1<<capNetAdmin) != 0
}
