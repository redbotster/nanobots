package secrets

import "github.com/redbotster/nanobots/internal/oneclaw"

// OneClaw is a 1Claw vault — what this build had before there was a
// choice. Kept as one backend among several rather than the only path, so
// pasting a Slack token no longer requires a 1Claw account first.
type OneClaw struct {
	Client  *oneclaw.Client
	VaultID string
}

func (o *OneClaw) Describe() string {
	return "1Claw vault — encrypted, access-logged, and covered by whatever passkey policy your account has"
}

func (o *OneClaw) Get(key string) (string, bool, error) {
	val, err := o.Client.GetSecret(o.VaultID, key)
	if err != nil {
		// Matches internal/step.vaultToken's own existing behaviour: this
		// package does not try to distinguish "not found" from "vault
		// locked" from "network down" here, because the caller already
		// has to handle a real, typed VaultLockedError from oneclaw
		// itself (oneclaw.AsVaultLocked) either way — wrapping it again
		// behind found=false would hide that classification rather than
		// preserve it.
		return "", false, err
	}
	return val, true, nil
}

func (o *OneClaw) Put(key, value string) error {
	return o.Client.PutSecret(o.VaultID, key, value)
}

// Delete is not implemented: 1Claw's vault API this repo has verified
// (docs/1claw-feature-requests.md) has no DELETE for a single secret path,
// only for a whole vault — which this package will never do on someone's
// behalf. Overwriting with PutSecret("") is the closest available
// approximation and is deliberately not offered as a silent substitute for
// a real delete.
func (o *OneClaw) Delete(key string) error {
	return errNoDelete
}

var errNoDelete = &notImplementedError{"1Claw has no API to delete a single vault secret — see docs/secrets.md"}

type notImplementedError struct{ msg string }

func (e *notImplementedError) Error() string { return e.msg }

var _ Store = (*OneClaw)(nil)
