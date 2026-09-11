package google

import "github.com/redbotster/nanobots/internal/oneclaw"

// LoadClientID reads GOOGLE_OAUTH_CLIENT_ID from the same dotenv-style file
// as ONECLAW_API_KEY (path="" uses oneclaw.DefaultEnvFilePath — see this
// package's doc comment). Returns ("", nil) if it isn't set yet.
func LoadClientID(path string) (string, error) {
	return oneclaw.LoadEnvValue(path, "GOOGLE_OAUTH_CLIENT_ID")
}
