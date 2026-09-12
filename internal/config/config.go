package config

import (
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"strings"
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
	var miss []string
	get := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			miss = append(miss, k)
		}
		return v
	}
	HafezBase, HafezUser, HafezPass = get("HAFEZ_BASEURL"), get("HAFEZ_USERNAME"), get("HAFEZ_PASSWORD")
	Issuer, Realm, ClientID, Secret = get("AUTH_URL"), get("AUTH_REALM"), get("AUTH_CLIENTID"), get("AUTH_SECRET_CODE")
	if len(miss) > 0 {
		log.Printf("env missing %s — auth/hafez off, stream still runs", strings.Join(miss, ", "))
	}
}

func TokenURL() string {
	return Issuer + "realms/" + Realm + "/protocol/openid-connect/token"
}
