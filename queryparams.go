package queryparams

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/lzeroanalytics/go-util/ptr"
)

const (
	structTagName = "query"
)

// Marshaler is implemented by types that can encode themselves to a single
// query-string value (e.g. “abc=XYZ”).
type Marshaler interface {
	MarshalQueryParam() (string, error)
}

// Unmarshaler is implemented by types that can parse themselves from a single
// query-string value (e.g. “XYZ” → struct).
type Unmarshaler interface {
	UnmarshalQueryParam(string) error
}

// Marshal turns any struct into url.Values according to `url` tags.
// Supported field kinds: string, ints, uints, floats, bool, time.Time, slices of those.
func Marshal(src any) (url.Values, error) {
	v := reflect.ValueOf(src)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct but got %v", v.Type())
	}

	uv := url.Values{}
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		name, omitzero := parseTag(sf.Tag.Get(structTagName))

		if name == "-" {
			continue
		}

		if name == "" {
			name = strings.ToLower(sf.Name)
		}

		fv := v.Field(i)
		if !fv.IsValid() || !fv.CanInterface() {
			continue
		}

		if omitzero && fv.IsZero() {
			continue
		}

		if fv.Kind() == reflect.Slice {
			for j := 0; j < fv.Len(); j++ {
				s, err := toString(fv.Index(j))
				if err != nil {
					return uv, err
				}

				uv.Add(name, s)
			}
		} else {
			s, err := toString(fv)
			if err != nil {
				return uv, err
			}

			uv.Set(name, s)
		}
	}

	return uv, nil
}

// Unmarshal populates dst (pointer to struct) from url.Values.
func Unmarshal(values url.Values, out any) error {
	v, err := ptr.EnforcePtr(out)
	if err != nil {
		return err
	}

	if v.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct but got %v", v.Type())
	}

	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		name, _ := parseTag(sf.Tag.Get(structTagName))

		if name == "-" {
			continue
		}

		if name == "" {
			name = strings.ToLower(sf.Name[:1]) + sf.Name[1:]
		}

		strs, ok := values[name]
		if !ok || len(strs) == 0 {
			continue
		}

		fv := v.Field(i)
		if !fv.CanSet() {
			continue
		}

		// Handle slices
		if fv.Kind() == reflect.Slice {
			sliceType := fv.Type().Elem()
			newSlice := reflect.MakeSlice(fv.Type(), 0, len(strs))

			for _, s := range strs {
				elem := reflect.New(sliceType).Elem()

				if err := setFromString(elem, s); err != nil {
					return fmt.Errorf("field %q: %w", name, err)
				}

				newSlice = reflect.Append(newSlice, elem)
			}

			fv.Set(newSlice)
			continue
		}

		// Single value
		if err := setFromString(fv, strs[0]); err != nil {
			return fmt.Errorf("field %q: %w", name, err)
		}
	}
	return nil
}

func parseTag(tag string) (string, bool) {
	if tag == "" {
		return "", false
	}

	parts := strings.Split(tag, ",")
	name := parts[0]
	omitzero := false
	for _, p := range parts[1:] {
		if p == "omitzero" {
			omitzero = true
		}
	}

	return name, omitzero
}

func toString(v reflect.Value) (string, error) {
	marshaler, ok := implementsMarshaler(v)
	if ok {
		return marshaler.MarshalQueryParam()
	}

	switch v.Kind() {
	case reflect.String:
		return v.String(), nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10), nil

	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil

	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil

	default:
		if t, ok := v.Interface().(time.Time); ok {
			return t.Format(time.RFC3339), nil
		}

		return "", fmt.Errorf("unsupported type %s", v.Kind())
	}
}

func implementsMarshaler(v reflect.Value) (Marshaler, bool) {
	marshaler, ok := v.Interface().(Marshaler) // try the value as-is
	if !ok {
		if v.Kind() != reflect.Ptr && v.CanAddr() {
			marshaler, ok = v.Addr().Interface().(Marshaler) // try *T
			if !ok {
				return nil, false
			}
		} else {
			return nil, false
		}
	}

	return marshaler, true
}

func implementsUnmarshaler(v reflect.Value) (Unmarshaler, bool) {
	unmarshaler, ok := v.Interface().(Unmarshaler) // try the value as-is
	if !ok {
		if v.Kind() != reflect.Ptr && v.CanAddr() {
			unmarshaler, ok = v.Addr().Interface().(Unmarshaler) // try *T
			if !ok {
				return nil, false
			}
		} else {
			return nil, false
		}
	}

	return unmarshaler, true
}

func setFromString(v reflect.Value, s string) error {
	unmarshaler, ok := implementsUnmarshaler(v)
	if ok {
		return unmarshaler.UnmarshalQueryParam(s)
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
		return nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(s, 10, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetInt(i)
		return nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(s, 10, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetUint(u)
		return nil

	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, v.Type().Bits())
		if err != nil {
			return err
		}
		v.SetFloat(f)
		return nil

	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		v.SetBool(b)
		return nil

	default:
		if _, ok := v.Interface().(time.Time); ok {
			parsed, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return err
			}
			v.Set(reflect.ValueOf(parsed))
			return nil
		}

		return fmt.Errorf("unsupported kind %s", v.Kind())
	}
}
