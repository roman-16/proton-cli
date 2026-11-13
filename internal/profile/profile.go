package profile

import (
	"fmt"
	"strings"
)

const Default = "default"

const maxBytes = 64

type Name struct{ value string }

func (n Name) String() string {
	if n.value == "" {
		return Default
	}
	return n.value
}

func (n Name) FileName(ext string) string { return n.String() + ext }

func Parse(name string) (Name, error) {
	if name == "" {
		return Name{}, nil
	}
	if len(name) > maxBytes {
		return Name{}, fmt.Errorf("a profile name can be at most %d characters, and %q is %d",
			maxBytes, name, len(name))
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		alphanumeric := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if i == 0 && !alphanumeric {
			return Name{}, fmt.Errorf("a profile name has to start with a letter or a digit, and %q starts with %q",
				name, string(c))
		}
		if !alphanumeric && c != '.' && c != '_' && c != '-' {
			return Name{}, fmt.Errorf("a profile name can hold letters, digits, %q, %q and %q, and %q holds %q",
				".", "_", "-", name, printable(c))
		}
	}
	return Name{value: name}, nil
}

func MustParse(name string) Name {
	n, err := Parse(name)
	if err != nil {
		panic(err)
	}
	return n
}

func printable(c byte) string {
	if c < 0x20 || c == 0x7f {
		return fmt.Sprintf("\\x%02x", c)
	}
	return string(c)
}

func (n Name) Equal(o Name) bool { return n.String() == o.String() }

func (n Name) IsDefault() bool { return n.String() == Default }

func (n Name) SignIn() string {
	if n.IsDefault() {
		return "proton account login"
	}
	return "proton account login --profile " + n.String()
}

func Names(raw []string) ([]Name, error) {
	out := make([]Name, 0, len(raw))
	for _, r := range raw {
		n, err := Parse(strings.TrimSpace(r))
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
