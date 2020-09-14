package recover

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/internal/utils"
)

// go test -run Test_Recover
func Test_Recover(t *testing.T) {
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			utils.AssertEqual(t, "Hi, I'm an error!", err.Error())
			return c.SendStatus(fiber.StatusTeapot)
		},
	})

	app.Use(New())

	app.Get("/panic", func(c *fiber.Ctx) error {
		panic("Hi, I'm an error!")
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/panic", nil))
	utils.AssertEqual(t, nil, err)
	utils.AssertEqual(t, fiber.StatusTeapot, resp.StatusCode)
}
