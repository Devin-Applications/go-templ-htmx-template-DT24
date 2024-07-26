package main

import (
	"app/views"
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/labstack/echo/v4"
)

//go:embed static
var static embed.FS

func Static() http.Handler {
	dist, err := fs.Sub(static, "static")
	if err != nil {
		panic(err)
	}

	return http.FileServer(http.FS(dist))
}

type Pet struct {
	ID      string
	Name    string
	Species string
	Age     int
}

var pets = make(map[string]Pet)

func createLogFile() *os.File {
	logFile, err := os.OpenFile("server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	return logFile
}

func addPet(c echo.Context) error {
	name := c.FormValue("name")
	species := c.FormValue("species")
	age, err := strconv.Atoi(c.FormValue("age"))
	if err != nil {
		c.Logger().Errorf("Invalid age input: %v", err)
		return c.String(http.StatusBadRequest, "Invalid age")
	}

	id := fmt.Sprintf("%d", len(pets)+1)
	pet := Pet{ID: id, Name: name, Species: species, Age: age}
	pets[id] = pet

	c.Logger().Infof("Pet added: %+v", pet)
	return c.Render(http.StatusOK, "petListItem", pet)
}

func deletePet(c echo.Context) error {
	id := c.Param("id")
	delete(pets, id)
	c.Logger().Infof("Pet with ID %s deleted", id)
	return c.NoContent(http.StatusOK)
}

func editPet(c echo.Context) error {
	id := c.Param("id")
	name := c.FormValue("name")
	species := c.FormValue("species")
	age, err := strconv.Atoi(c.FormValue("age"))
	if err != nil {
		c.Logger().Errorf("Invalid age input for edit: %v", err)
		return c.String(http.StatusBadRequest, "Invalid age")
	}

	pet := Pet{ID: id, Name: name, Species: species, Age: age}
	pets[id] = pet

	c.Logger().Infof("Pet edited: %+v", pet)
	return c.Render(http.StatusOK, "petListItem", pet)
}

func getEditPetForm(c echo.Context) error {
    id := c.Param("id")
    pet, ok := pets[id]
    if !ok {
        c.Logger().Warnf("Pet with ID %s not found for editing", id)
        return c.String(http.StatusNotFound, "Pet not found")
    }
    c.Logger().Infof("Edit form requested for pet: %+v", pet)
    return c.Render(http.StatusOK, "editPetForm", pet)
}

type TemplRenderer struct {
	templates map[string]func(interface{}) templ.Component
}

func (t *TemplRenderer) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	componentFunc, ok := t.templates[name]
	if !ok {
		return fmt.Errorf("template %s not found", name)
	}
	component := componentFunc(data)
	return component.Render(c.Request().Context(), w)
}

func main() {
	logFile := createLogFile()
	defer logFile.Close()

	e := echo.New()
	e.Logger.SetOutput(logFile)
	e.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", Static())))

	// Register TemplRenderer
	e.Renderer = &TemplRenderer{
		templates: map[string]func(interface{}) templ.Component{
			"petListItem": func(data interface{}) templ.Component {
				pet, ok := data.(Pet)
				if !ok {
					return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
						return fmt.Errorf("invalid data type for petListItem")
					})
				}
				return views.PetListItem(pet.ID, pet.Name, pet.Species, pet.Age)
			},
			"editPetForm": func(data interface{}) templ.Component {
				pet, ok := data.(Pet)
				if !ok {
					return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
						return fmt.Errorf("invalid data type for editPetForm")
					})
				}
				return views.EditPetForm(pet.ID, pet.Name, pet.Species, pet.Age)
			},
		},
	}

	// HTMX handlers
	e.POST("/add-pet", addPet)
	e.DELETE("/delete-pet/:id", deletePet)
	e.POST("/edit-pet/:id", editPet)
	e.GET("/edit-pet/:id", getEditPetForm)

	// bind views to the server
	views.Routes(e)

	// Start server
	go func() {
		if err := e.Start(fmt.Sprintf(":%s", os.Getenv("PORT"))); err != nil && err != http.ErrServerClosed {
			e.Logger.Error("shutting down the server", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with a timeout of 10 seconds.
	// Use a buffered channel to avoid missing signals as recommended for signal.Notify
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		e.Logger.Fatal(err)
	}
}
