package main

import (
	"fmt"
	"log"

	"github.com/swasthikshetty10/go-ratelimiter/limiter"
	_ "github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
)

func main() {
	l, err := limiter.NewInMemory(
		limiter.AlgorithmLeakyBucket,
		limiter.WithRate(10),
		limiter.WithCapacity(100),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(l.Allow())
}
