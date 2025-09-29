package main

import (
    `net/http`
    `github.com/zaoangod/tiny/router`
)

func main() {
    m := router.New()
    m.Get("/system/user/list", func(response http.ResponseWriter, request *http.Request) {
        response.Write([]byte("hello world"))
    })
    // log.Fatal(http.ListenAndServe(":8090", m))
}
