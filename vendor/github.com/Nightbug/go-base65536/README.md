# go-base65536 [![GoDoc](https://godoc.org/github.com/Nightbug/go-base65536?status.svg)](https://godoc.org/github.com/Nightbug/go-base65536)

Go library for encoding data into [base65536](https://github.com/ferno/base65536).

## Examples

Marshaling

```go
package main

import (
	"fmt"
	"github.com/Nightbug/go-base65536"
)

func main() {
	fmt.Println(base65536.Marshal([]byte("hello world")))
}
```

Unmarshaling

```go
package main

import (
	"fmt"
	"github.com/Nightbug/go-base65536"
)

func main() {
	var out []byte
	err := base65536.Marshal([]byte("驨ꍬ啯𒁷ꍲᕤ"), &out)
	if err != nil {
		panic(err)
	}
}
```

## License

MIT
