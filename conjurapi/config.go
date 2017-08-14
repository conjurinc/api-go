package conjurapi

import (
	"fmt"
	"reflect"
	"strings"
)

type Config struct {
	Account        string `validate:"required"`
	APIKey         string
	ApplianceURL   string `validate:"required"`
	Login          string
	AuthnTokenFile string
}

const tagName = "validate"

func (c Config) validate() (error) {
	v := reflect.ValueOf(c)
	errors := []string{}

	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		tag := f.Tag.Get(tagName)

		switch tag {
		case "required":
			val := v.Field(i).Interface()
			if val.(string) == "" {
				errors = append(errors, fmt.Sprintf("%s is required.", f.Name))
			}
		default:
		}
	}

	if len(errors) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errors, "\n"))
}

