// This binary exports consumer-owned effects; it does not load any graph files.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	distributed "example.com/meshwork-prototype/examples/distributed"
	"example.com/meshwork-prototype/plugins/remote"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8081", "executor listen address")
	tokenEnv := flag.String("token-env", "MESHWORK_EXECUTOR_TOKEN", "authorization token environment variable")
	flag.Parse()
	handler, err := remote.ExecutorHandler(distributed.Implementations(), os.Getenv(*tokenEnv))
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("executor listening on %s", *listen)
	log.Fatal(server.ListenAndServe())
}
