# httpsrv
An extremely simple Go library to resolve SRV records in HTTP requests

## Usage

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/tired-engineer/httpsrv"
)

func main() {
	transport := &http.Transport{}

	httpsrv.AddSRVRoundTripper(nil, transport)

	client := &http.Client{Transport: transport}

	resp, err := client.Get("http+srv://some-service.query.consul/healthz")

	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	fmt.Println(resp.Status)
}

```
