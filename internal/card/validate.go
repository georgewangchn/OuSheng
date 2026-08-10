package card

import (
	"fmt"
	"regexp"
	"strings"
)

const MaxCardBytes = 8192

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var kinds = map[string]bool{"http": true, "cli": true, "lib": true, "event": true}

var statuses = map[Status]bool{Proposed: true, Agreed: true, Live: true, Verified: true, Deprecated: true}

var forbidden = []string{"```", "Traceback (most recent call last)"}

func Validate(c Card, raw []byte) error {
	if !idRe.MatchString(c.ID) {
		return fmt.Errorf("invalid id %q", c.ID)
	}
	if strings.TrimSpace(c.Owner) == "" {
		return fmt.Errorf("owner required")
	}
	if strings.TrimSpace(c.Task) == "" {
		return fmt.Errorf("task required")
	}
	if !statuses[c.Status] {
		return fmt.Errorf("invalid status %q", c.Status)
	}
	if c.Version < 0 {
		return fmt.Errorf("version must be >= 0")
	}
	if !kinds[c.Contract.Kind] {
		return fmt.Errorf("invalid contract.kind %q", c.Contract.Kind)
	}
	if c.Status == Verified {
		if c.Evidence == nil || c.Evidence.Probe == "" || c.Evidence.PassedAtCommit == "" || c.Evidence.By == "" {
			return fmt.Errorf("status=verified requires complete evidence")
		}
	}
	if c.Contract.Breaking && c.HumanAck == nil {
		return fmt.Errorf("contract.breaking=true requires human_ack")
	}
	if len(raw) > MaxCardBytes {
		return fmt.Errorf("card exceeds %d bytes", MaxCardBytes)
	}
	for _, f := range forbidden {
		if strings.Contains(string(raw), f) {
			return fmt.Errorf("forbidden content: %q", f)
		}
	}
	return nil
}
