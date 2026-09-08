package config

import (
	"crypto/tls"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

// ponytail: skip TLS verify to match nestapi-milestone Hafez client.
var HTTP = &http.Client{
	Timeout:   15 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
}

var (
	ClientID, Secret, Realm                 string
	HafezBase, HafezUser, HafezPass, Issuer string
)

func Load() {
	_ = godotenv.Load()
	HafezBase, HafezUser, HafezPass = must("HAFEZ_BASEURL"), must("HAFEZ_USERNAME"), must("HAFEZ_PASSWORD")
	Issuer, Realm, ClientID, Secret = must("AUTH_URL"), must("AUTH_REALM"), must("AUTH_CLIENTID"), must("AUTH_SECRET_CODE")
}

func TokenURL() string {
	return Issuer + "realms/" + Realm + "/protocol/openid-connect/token"
}

func must(k string) string {
	v := os.Getenv(k)
	if v == "" {
		panic("missing env " + k)
	}
	return v
}
