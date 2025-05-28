# httpsrv
An extremely simple Go library to resolve SRV records in HTTP requests

## Process

When a request is made to a URL with the `http+srv` or `https+srv` scheme, the library will resolve the SRV record for the service and use the first result (host and port) to make the request.

For example, the following URL:

`http+srv://some-service.query.consul/healthz`

Will be resolved to:

`http://node1.consul.:8080/healthz`

> [!NOTE]
> Please keep in mind that the port number will be overwritten by the SRV record.

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
