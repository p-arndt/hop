// Command twofactorcode prints the verification code the two-factor test
// container is currently expecting, refreshed every second with the time left in
// the window.
//
// It stands in for the phone you would normally read the code off, so hop's 2FA
// card can be tried by hand rather than only by the tests:
//
//	docker run -d --name hop-2fa -p 127.0.0.1:22220:2222 hop-twofactor:test
//	go run ./tools/twofactorcode
//
// The secret is the fixed one baked into that image (see internal/dockerenv and
// its testdata/twofactor), which authenticates a throwaway account in a
// loopback-only container. This is a development aid for that container and
// nothing else: it has no way to read a real account's secret.
package main

import (
	"fmt"
	"time"

	"hop/internal/dockerenv"
)

func main() {
	fmt.Printf("hop two-factor test container — user %q, password %q\n\n",
		dockerenv.User, dockerenv.Password)

	for {
		left := 30 - time.Now().Unix()%30
		fmt.Printf("\r  code %s   valid for %2ds  ", dockerenv.Code(), left)
		time.Sleep(time.Second)
	}
}
