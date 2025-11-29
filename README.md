# go-clir

`clir` is a lightweight, composable router for building command-line interfaces in Go.

It provides:

- HTTP-style routing for CLI arguments
- Named parameters and extra trailing args
- Middleware chaining
- Nested route groups
- Typed context resolution (`WithContext`, `WithChildContext`)
- A tiny API inspired by `chi`

## Install

```bash
go get github.com/yourname/go-clir
```

## Basic Example

```go
package main

import (
    "context"
    "fmt"

    "github.com/yourname/go-clir"
)

func main() {
    r := clir.New()

    r.Routes(func(b *clir.Builder) {
        b.Handle("hello", "Say hello", func(req *clir.Request) error {
            fmt.Println("hello world")
            return nil
        })
    })

    _ = r.Run(context.Background(), []string{"hello"})
}
```

## Routing With Parameters & Extra Arguments

```go
r := clir.New()

r.Routes(func(b *clir.Builder) {
    b.Handle("comp <component> build", "Build a component", func(req *clir.Request) error {
        fmt.Printf("component=%s extra=%v\n", req.Params["component"], req.Extra)
        return nil
    })
})

_ = r.Run(context.Background(), []string{"comp", "api", "build", "--tag", "latest"})
// component=api extra=[--tag latest]
```

## Middleware

```go
log := func(label string) clir.Middleware {
    return func(next clir.Handler) clir.Handler {
        return func(req *clir.Request) error {
            fmt.Println("before", label)
            err := next(req)
            fmt.Println("after", label)
            return err
        }
    }
}

r := clir.New()

r.Routes(func(b *clir.Builder) {
    b.With(
        log("outer"),
        log("inner"),
    ).Handle("run", "Run", func(req *clir.Request) error {
        fmt.Println("handler")
        return nil
    })
})

_ = r.Run(context.Background(), []string{"run"})
// before outer
// before inner
// handler
// after inner
// after outer
```

## Typed Contexts

### Single Layer

```go
type App struct{ Name string }

resolveApp := func(req *clir.Request) (App, error) {
    return App{Name: "cli-app"}, nil
}

r := clir.New()

r.Routes(func(b *clir.Builder) {
    app := clir.WithContext(b, resolveApp)

    app.Handle("ping", "Ping the app", func(req *clir.Request, ctx App) error {
        fmt.Println("app:", ctx.Name)
        return nil
    })
})

_ = r.Run(context.Background(), []string{"ping"})
// app: cli-app
```

### Layered Contexts (`WithChildContext`)

```go
type App struct{ Name string }
type Component struct {
    App  App
    Name string
}

resolveApp := func(req *clir.Request) (App, error) {
    return App{Name: "cli-app"}, nil
}

resolveComponent := func(app App, req *clir.Request) (Component, error) {
    return Component{
        App:  app,
        Name: req.Params["component"],
    }, nil
}

r := clir.New()

r.Routes(func(b *clir.Builder) {
    app := clir.WithContext(b, resolveApp)

    app.Route("comp <component>", func(b *clir.ContextBuilder[App]) {
        comp := clir.WithChildContext(b, resolveComponent)

        comp.Route("image", func(b *clir.ContextBuilder[Component]) {
            b.Handle("build", "Build images", func(req *clir.Request, c Component) error {
                fmt.Printf("app=%s comp=%s\n", c.App.Name, c.Name)
                return nil
            })
        })
    })
})

_ = r.Run(context.Background(), []string{"comp", "api", "image", "build"})
// app=cli-app comp=api
```

## Printing Help

```go
r := clir.New()

r.Routes(func(b *clir.Builder) {
    b.Handle("hello", "Say hello", func(req *clir.Request) error {
        fmt.Println("hello world")
        return nil
    })
})

r.PrintHelp(os.Stdout)

// Available commands:
//   hello   Say hello
```
