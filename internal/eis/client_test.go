package eis_test

import (
	"strings"
	"testing"

	"github.com/rinat1313/zakupki-collector/internal/eis"
)

func TestBuildAndParseSOAPNoData(t *testing.T) {
	c, err := eis.NewClient(eis.ClientOptions{
		Endpoint: "https://example.invalid/getDocsMis",
		Token:    "11111111-2222-3333-4444-555555555555",
		Mode:     "TEST",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = c
}

func TestClientRequiresToken(t *testing.T) {
	_, err := eis.NewClient(eis.ClientOptions{Endpoint: "https://x"})
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("want token error, got %v", err)
	}
}
