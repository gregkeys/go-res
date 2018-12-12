/*
This is an example of how to create a RES service with collections.
* It exposes a collection: "library.books".
* It allows setting the books' Title and Author property through the "set" method.
* It allows creating new books that are added to the collection
* It allows deleting existing books from the collection
* It verifies that a title and author is always set
*/
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/jirenius/go-res"
)

// Book represents a book model
type Book struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Author string `json:"author"`
}

// Map of all book models
var bookModels = map[string]*Book{
	"library.book.1": {ID: 1, Title: "Animal Farm", Author: "George Orwell"},
	"library.book.2": {ID: 2, Title: "Brave New World", Author: "Aldous Huxley"},
	"library.book.3": {ID: 3, Title: "Coraline", Author: "Neil Gaiman"},
}

// Collection of books
var books = []res.Ref{
	res.Ref("library.book.1"),
	res.Ref("library.book.2"),
	res.Ref("library.book.3"),
}

// ID counter for new book models
var nextBookID int64 = 4

func main() {
	// Create a new RES Service
	s := res.NewService("library")

	// Add handlers for "library.book.$id" models
	s.Handle(
		"book.$id",
		res.Access(res.AccessGranted),
		res.GetModel(getBookHandler),
		res.Set(setBookHandler),
	)

	// Add handlers for "library.books" collection
	s.Handle(
		"books",
		res.Access(res.AccessGranted),
		res.GetCollection(getBooksHandler),
		res.New(newBookHandler),
		res.Call("delete", deleteBookHandler),
	)

	// Start service in separate goroutine
	stop := make(chan bool)
	go func() {
		defer close(stop)
		if err := s.ListenAndServe("nats://localhost:4222"); err != nil {
			fmt.Printf("%s\n", err.Error())
		}
	}()

	// Run a simple webserver to serve the client.
	// This is only for the purpose of making the example easier to run.
	go func() { log.Fatal(http.ListenAndServe(":8082", http.FileServer(http.Dir("./")))) }()
	fmt.Println("Client at: http://localhost:8082/")

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	select {
	case <-c:
		// Graceful stop
		s.Shutdown()
	case <-stop:
	}
}

func getBookHandler(w res.GetModelResponse, r *res.Request) {
	book := bookModels[r.ResourceName]
	if book == nil {
		w.NotFound()
		return
	}
	w.Model(book)
}

func setBookHandler(w res.CallResponse, r *res.Request) {
	book := bookModels[r.ResourceName]
	if book == nil {
		w.NotFound()
		return
	}

	// Unmarshal parameters to a anonymous struct
	var p struct {
		Title  *string `json:"title,omitempty"`
		Author *string `json:"author,omitempty"`
	}
	r.UnmarshalParams(&p)

	changed := make(map[string]interface{}, 2)

	// Check if the title property was changed
	if p.Title != nil {
		// Verify it is not empty
		title := strings.TrimSpace(*p.Title)
		if title == "" {
			w.InvalidParams("Title must not be empty")
			return
		}

		if title != book.Title {
			// Update the model.
			book.Title = title
			changed["title"] = title
		}
	}

	// Check if the author property was changed
	if p.Author != nil {
		// Verify it is not empty
		author := strings.TrimSpace(*p.Author)
		if author == "" {
			w.InvalidParams("Author must not be empty")
			return
		}
		if author != book.Author {
			// Update the model.
			book.Author = author
			changed["author"] = author
		}
	}

	// Send a change event with updated fields
	r.ChangeEvent(changed)

	// Send success response
	w.OK(nil)
}

func getBooksHandler(w res.GetCollectionResponse, r *res.Request) {
	w.Collection(books)
}

func newBookHandler(w res.NewResponse, r *res.Request) {
	var p struct {
		Title  string `json:"title"`
		Author string `json:"author"`
	}
	r.UnmarshalParams(&p)

	// Trim whitespace
	title := strings.TrimSpace(p.Title)
	author := strings.TrimSpace(p.Author)

	// Check if we received both title and author
	if title == "" || author == "" {
		w.InvalidParams("Must provide both title and author")
		return
	}
	// Create a new book model
	rid := fmt.Sprintf("library.book.%d", nextBookID)
	book := &Book{ID: nextBookID, Title: title, Author: author}
	nextBookID++
	bookModels[rid] = book

	// Convert resource ID to a resource reference
	ref := res.Ref(rid)
	// Send add event
	r.AddEvent(ref, len(books))
	// Appends the book reference to the collection
	books = append(books, ref)

	// Respond with a reference to the newly created book model
	w.New(ref)
}

func deleteBookHandler(w res.CallResponse, r *res.Request) {
	var p struct {
		ID int64 `json:"id,omitempty"`
	}
	r.UnmarshalParams(&p)

	rname := fmt.Sprintf("library.book.%d", p.ID)

	// Ddelete book if it exist
	if _, ok := bookModels[rname]; ok {
		delete(bookModels, rname)
		// Find the book in books collection, and remove it
		for i, rid := range books {
			if rid == res.Ref(rname) {
				// Remove it from slice
				books = append(books[:i], books[i+1:]...)
				// Send remove event
				r.RemoveEvent(i)

				break
			}
		}
	}

	// Send success response. It is up to the service to define if a delete
	// should be idempotent or not. In this case we send success regardless
	// if the book existed or not, making it idempotent.
	w.OK(nil)
}

// createBook creates a new Book model, assigns it a unique ID,
// and adds it to the bookModels map.
// It returns the resource ID.
func newBook(title string, author string) string {
	rid := fmt.Sprintf("library.book.%d", nextBookID)
	book := &Book{ID: nextBookID, Title: title, Author: author}
	nextBookID++
	bookModels[rid] = book
	return rid
}
