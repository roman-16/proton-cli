package pass

import (
	"fmt"
	"strings"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/otp"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// Extra fields, and the sections some item types group them under.
//
// Pass lets an item carry named fields beyond the ones its type defines, and
// lets a few types put those fields under headings - "Recovery", "Router", the
// second passport. A field states its own section rather than the command line
// having a mode, so `--field` stays one self-contained token that can be given
// in any order and read back exactly as it was written.

// sectionSeparator divides a field's section from its name.
const sectionSeparator = "/"

// A field belongs to a section, or to no section, and is one of the three kinds
// Pass's editor offers.
type extraField struct {
	section string
	name    string
	value   string
	kind    fieldKind
}

// fieldKind is what a custom field holds: text anyone reading the item can see,
// a value kept hidden until asked for, or a two-factor secret Pass turns into a
// code.
type fieldKind int

const (
	fieldText fieldKind = iota
	fieldHidden
	fieldTOTP
)

// flag is the flag that writes a field of this kind.
func (k fieldKind) flag() string {
	switch k {
	case fieldHidden:
		return "hidden"
	case fieldTOTP:
		return "totp-field"
	}
	return "field"
}

// parseExtraField reads "Recovery/Code=1234" or "Code=1234".
//
// The name may not be empty, and neither may the section when one is stated: a
// field under a heading with no words is one nothing can be said about.
func parseExtraField(raw string, kind fieldKind) (extraField, error) {
	flag := kind.flag()
	name, value, ok := strings.Cut(raw, "=")
	if !ok {
		return extraField{}, errs.Problemf("invalid --%s %q (expected NAME=VALUE, or SECTION%sNAME=VALUE)",
			flag, raw, sectionSeparator)
	}
	f := extraField{name: strings.TrimSpace(name), value: value, kind: kind}
	if section, rest, split := strings.Cut(f.name, sectionSeparator); split {
		f.section, f.name = strings.TrimSpace(section), strings.TrimSpace(rest)
		if f.section == "" {
			return extraField{}, errs.Problemf("invalid --%s %q (the section has no name)", flag, raw)
		}
	}
	if f.name == "" {
		return extraField{}, errs.Problemf("invalid --%s %q (the field has no name)", flag, raw)
	}
	// A two-factor secret nothing can read is a field that will never produce a
	// code, and whether it can be read is plain from the value itself.
	if f.kind == fieldTOTP {
		if _, err := otp.Parse(f.value); err != nil {
			return extraField{}, errs.Problemf("invalid --%s %q: %v", flag, raw, err)
		}
	}
	return f, nil
}

// pb builds the stored field.
func (f extraField) pb() *pb.ExtraField {
	out := &pb.ExtraField{FieldName: f.name}
	switch f.kind {
	case fieldHidden:
		out.Content = &pb.ExtraField_Hidden{Hidden: &pb.ExtraHiddenField{Content: f.value}}
	case fieldTOTP:
		out.Content = &pb.ExtraField_Totp{Totp: &pb.ExtraTotp{TotpUri: f.value}}
	default:
		out.Content = &pb.ExtraField_Text{Text: &pb.ExtraTextField{Content: f.value}}
	}
	return out
}

// ExtraFields are the custom fields a command line asked for, by kind.
type ExtraFields struct {
	Text   []string
	Hidden []string
	TOTP   []string
}

// Empty reports whether any field was named at all.
func (e ExtraFields) Empty() bool {
	return len(e.Text) == 0 && len(e.Hidden) == 0 && len(e.TOTP) == 0
}

// CheckFields reports whether every --field and --hidden can be read.
//
// It is answerable from the command line alone, so a command asks before it
// signs in rather than after a round trip that was never needed.
func CheckFields(in ExtraFields) error {
	_, err := parseExtraFields(in)
	return err
}

// CheckSections reports whether an item of this type has anywhere to put the
// sections those fields name. Which types group their fields is known from
// --type, so this too is settled before the network.
func CheckSections(itemType string, in ExtraFields) error {
	if SectionsAllowed(itemType) {
		return nil
	}
	fields, err := parseExtraFields(in)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.section != "" {
			return fmt.Errorf("a %s item has no sections, so %q has nowhere to go",
				itemType, FieldRef(f.section, f.name))
		}
	}
	return nil
}

// parseExtraFields reads them into the order they were given, which is the order
// they are stored and shown in.
func parseExtraFields(in ExtraFields) ([]extraField, error) {
	out := make([]extraField, 0, len(in.Text)+len(in.Hidden)+len(in.TOTP))
	for _, group := range []struct {
		raw  []string
		kind fieldKind
	}{{in.Text, fieldText}, {in.Hidden, fieldHidden}, {in.TOTP, fieldTOTP}} {
		for _, raw := range group.raw {
			f, err := parseExtraField(raw, group.kind)
			if err != nil {
				return nil, err
			}
			out = append(out, f)
		}
	}
	return out, nil
}

// split separates the fields that belong to no section from the sections, which
// keep the order their first field appeared in.
func split(fields []extraField) (loose []*pb.ExtraField, sections []*pb.CustomSection) {
	at := map[string]*pb.CustomSection{}
	for _, f := range fields {
		if f.section == "" {
			loose = append(loose, f.pb())
			continue
		}
		s, ok := at[f.section]
		if !ok {
			s = &pb.CustomSection{SectionName: f.section}
			at[f.section] = s
			sections = append(sections, s)
		}
		s.SectionFields = append(s.SectionFields, f.pb())
	}
	return loose, sections
}

// SectionsAllowed reports whether an item of this type can group its fields.
//
// Proton stores sections on the types whose editor offers them, and nowhere
// else: a login's fields are a flat list. A section given to a type that has no
// place for one would be dropped on the way in, so it is refused instead.
func SectionsAllowed(itemType string) bool {
	switch itemType {
	case "custom", "ssh-key", "wifi", "identity":
		return true
	}
	return false
}

// sectionsOf reads back the sections an item carries, whichever type it is.
func sectionsOf(content *pb.Content) []*pb.CustomSection {
	switch c := content.GetContent().(type) {
	case *pb.Content_Custom:
		return c.Custom.GetSections()
	case *pb.Content_SshKey:
		return c.SshKey.GetSections()
	case *pb.Content_Wifi:
		return c.Wifi.GetSections()
	case *pb.Content_Identity:
		return c.Identity.GetExtraSections()
	}
	return nil
}

// setSections stores them on whichever type holds them, and reports whether the
// type had somewhere to put them.
func setSections(content *pb.Content, sections []*pb.CustomSection) bool {
	switch c := content.GetContent().(type) {
	case *pb.Content_Custom:
		c.Custom.Sections = sections
	case *pb.Content_SshKey:
		c.SshKey.Sections = sections
	case *pb.Content_Wifi:
		c.Wifi.Sections = sections
	case *pb.Content_Identity:
		c.Identity.ExtraSections = sections
	default:
		return false
	}
	return true
}

// FieldRef renders a field the way --field accepts it, so what a record shows
// can be handed straight back to a command.
func FieldRef(section, name string) string {
	if section == "" {
		return name
	}
	return section + sectionSeparator + name
}

// patchExtraFields lays the named fields over what an item already holds.
//
// A field is identified by its section and its name together, so setting
// "Router/Password" leaves "Wifi/Password" alone. One that names nothing already
// there is added, which is how a field is created on an item that has none.
func patchExtraFields(it *pb.Item, patch Patch) error {
	fields, err := parseExtraFields(patch.ExtraFields)
	if err != nil {
		return err
	}
	for _, f := range fields {
		if f.section == "" {
			it.ExtraFields = replaceOrAppend(it.ExtraFields, f)
			continue
		}
		if !putInSection(it.Content, f) {
			return fmt.Errorf("a %s item has no sections to put a field under", itemTypeName(it))
		}
	}
	return nil
}

// replaceOrAppend sets the field with this name, or adds it.
func replaceOrAppend(in []*pb.ExtraField, f extraField) []*pb.ExtraField {
	for i, existing := range in {
		if existing.GetFieldName() == f.name {
			in[i] = f.pb()
			return in
		}
	}
	return append(in, f.pb())
}

// putInSection sets the field within its section, creating the section if the
// item does not have one by that name. It reports whether this kind of item can
// hold sections at all.
func putInSection(content *pb.Content, f extraField) bool {
	sections := sectionsOf(content)
	for _, s := range sections {
		if s.GetSectionName() == f.section {
			s.SectionFields = replaceOrAppend(s.GetSectionFields(), f)
			return setSections(content, sections)
		}
	}
	sections = append(sections, &pb.CustomSection{
		SectionName: f.section, SectionFields: []*pb.ExtraField{f.pb()},
	})
	return setSections(content, sections)
}
