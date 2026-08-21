// Command twofactorcode prints the code the two-factor test container expects.
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
