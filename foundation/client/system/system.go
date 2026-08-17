package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

/*

"You have this HTTP handler. A request comes in, times out after 1 second,
 and the handler returns. But the goroutine inside getUser is still
running. What exactly is happening, why is this catastrophic at scale,
and how do you fix it correctly?"

*/

// one should know how to exit the go routine before writing one

type User struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func getUser(ctx context.Context, id int) <-chan User {
	ch := make(chan User, 1)
	go func() {
		defer close(ch)
		// time.Sleep(time.Millisecond * 100) // takes 2 seconds
		select {
		case <-time.After(2 * time.Microsecond):
			ch <- User{ID: id, Name: "Hi"}
		case <-ctx.Done():
			// Caller gave up, exit immediately to prevent leak
			return
		}
	}()
	return ch
}

func handler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ch := getUser(ctx, 5)
	select {
	case user, ok := <-ch:
		if !ok {
			return
		}
		json.NewEncoder(w).Encode(user)
	case <-ctx.Done():
		http.Error(w, "timeout", 504)
		return
	}
}

func main() {
	http.HandleFunc("/user", handler)

	fmt.Println("Server starting on :8085...")
	fmt.Println("Try visiting: http://localhost:8085/user")

	// Start the server
	if err := http.ListenAndServe(":8085", nil); err != nil {
		fmt.Printf("Server failed: %s\n", err)
	}
}
