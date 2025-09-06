# goshut

> A Golang library for graceful shutdowns

## usage

### basic usage

```golang
import (
	"context"
	"github.com/dvjn/goshut"
)

func main() {
    gs := goshut.New()

    gs.Register(func(ctx context.Context) error {
        // cleanup logic
        return nil
    })
    gs.Register(/* other cleanup functions */)

    gs.WaitAndShutdown()
}
```

### custom configuration

```golang
import (
	"context"
	"time"
	"github.com/dvjn/goshut"
)

func main() {
    // configure with custom timeout
    gs := goshut.New(
        goshut.WithTimeout(10 * time.Second),
    )

    gs.Register(/* cleanup function */)
    
    gs.WaitAndShutdown()
}
```

## options

- `WithTimeout(duration)` - sets the shutdown timeout (default: 30s)
- `WithLogger(logger logr.Logger)` - sets a logger for shutdown messages (default: no logging)
- `WithSignals(signals ...os.Signal)` - sets the signals to wait for (default: SIGINT, SIGTERM, SIGQUIT)
