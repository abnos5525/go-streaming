package config

import "testing"

func TestLoadMissingNoPanic(t *testing.T) {
	for _, k := range []string{
		"HAFEZ_BASEURL", "HAFEZ_USERNAME", "HAFEZ_PASSWORD",
		"AUTH_URL", "AUTH_REALM", "AUTH_CLIENTID", "AUTH_SECRET_CODE",
	} {
		t.Setenv(k, "")
	}
	Load()
}
