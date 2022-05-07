package transformer

import (
	"sort"
	"strconv"

	"github.com/dstotijn/go-notion"
)

type markdownPropertyWriter func(*markdownEnv, string, notion.DatabasePageProperty)

var markdownPropertyMapper = map[notion.DatabasePropertyType]markdownPropertyWriter{
	notion.DBPropTypeTitle:       markdownPropTitle,
	notion.DBPropTypeRichText:    markdownPropRichText,
	notion.DBPropTypeNumber:      markdownPropNumber,
	notion.DBPropTypeSelect:      markdownPropSelect,
	notion.DBPropTypeMultiSelect: markdownPropMultiSelect,
	notion.DBPropTypeDate:        markdownPropDate,
	//notion.DBPropTypePeople         DatabasePropertyType = "people"
	//notion.DBPropTypeFiles          DatabasePropertyType = "files"
	notion.DBPropTypeCheckbox: markdownPropCheckbox,
	notion.DBPropTypeURL:      markdownPropURL,
	//notion.DBPropTypeEmail          DatabasePropertyType = "email"
	//notion.DBPropTypePhoneNumber    DatabasePropertyType = "phone_number"
	//notion.DBPropTypeFormula        DatabasePropertyType = "formula"
	notion.DBPropTypeRelation: markdownPropRelation,
	//notion.DBPropTypeRollup         DatabasePropertyType = "rollup"
	notion.DBPropTypeCreatedTime:    markdownPropCreatedTime,
	notion.DBPropTypeCreatedBy:      markdownPropCreatedBy,
	notion.DBPropTypeLastEditedTime: markdownPropLastEditedTime,
	notion.DBPropTypeLastEditedBy:   markdownPropLastEditedBy,
}

func (m *Markdown) isFrontMatterType(t notion.DatabasePropertyType) bool {
	switch t {
	case notion.DBPropTypeRichText, notion.DBPropTypeRelation:
		return false
	case notion.DBPropTypeSelect, notion.DBPropTypeMultiSelect:
		return !m.config.SelectToTags
	default:
		return true
	}
}

func (m *Markdown) transformFrontMatter(env *markdownEnv, page *notion.Page) {
	if m.config.NoFrontMatters && m.config.NoAlias {
		return
	}

	env.b.WriteString("---\n")

	if !m.config.NoAlias {
		env.b.WriteString("aliases: ")
		env.b.WriteString(SimpleID(page.ID))
		env.b.WriteString("\n")
	}

	if m.config.NoFrontMatters { // skip front matters
		return
	}

	props, ok := page.Properties.(notion.DatabasePageProperties)
	if !ok {
		env.b.WriteString("---\n\n")
		return
	}

	keys := m.config.FrontMatters
	if len(keys) == 0 {
		for key, prop := range props {
			if m.isFrontMatterType(prop.Type) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
	}

	nEnv := env.Copy()
	nEnv.indent = "  "

	for _, key := range keys {
		prop := props[key]

		writer, ok := markdownPropertyMapper[prop.Type]
		if !ok {
			continue
		}

		if m.config.TitleToH1 && prop.Type == notion.DBPropTypeTitle {
			continue
		}

		env.b.WriteString(key)
		env.b.WriteString(": ")
		writer(nEnv, key, prop)
	}

	env.b.WriteString("---\n\n")
}

func (m *Markdown) transformNonFrontMatter(env *markdownEnv, page *notion.Page) {
	props, ok := page.Properties.(notion.DatabasePageProperties)
	if !ok {
		return
	}

	keys := m.config.NonFrontMatters
	if len(keys) == 0 {
		for key, prop := range props {
			if !m.isFrontMatterType(prop.Type) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
	}

	nEnv := env.Copy()
	nEnv.indent = "  "

	for _, key := range keys {
		prop := props[key]

		writer, ok := markdownPropertyMapper[prop.Type]
		if !ok {
			continue
		}

		env.b.WriteString("- ")
		env.b.WriteString(key)
		env.b.WriteString(": ")
		writer(nEnv, key, prop)
	}

	if len(keys) > 0 {
		env.b.WriteString("\n") // spare line break needed
	}
}

func markdownPropTitle(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	for _, text := range prop.Title {
		env.b.WriteString(text.PlainText)
	}

	env.b.WriteString("\n")
}

func markdownPropRichText(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	for _, text := range prop.RichText {
		markdownAnnotation(env, text, true)
		env.b.WriteString(text.PlainText)
		markdownAnnotation(env, text, false)
	}

	env.b.WriteString("\n")
}

func markdownPropNumber(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	if prop.Number != nil {
		env.b.WriteString(strconv.FormatFloat(*prop.Number, 'f', 2, 64))
	}
	env.b.WriteString("\n")
}

func markdownPropSelect(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	if prop.Select != nil {
		if env.m.config.SelectToTags {
			env.b.WriteString("#")
		}

		env.b.WriteString(prop.Select.Name)
	}
	env.b.WriteString("\n")
}

func markdownPropMultiSelect(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	if prop.MultiSelect == nil {
		env.b.WriteString("\n")
		return
	}

	env.b.WriteString("\n")
	for _, item := range prop.MultiSelect {
		env.b.WriteString(env.indent)
		env.b.WriteString("- ")
		if env.m.config.SelectToTags {
			env.b.WriteString("#")
		}
		env.b.WriteString(item.Name)
		env.b.WriteString("\n")
	}
}

func markdownPropDate(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	if prop.Date == nil {
		env.b.WriteString("\n")
		return
	}

	env.b.WriteString("\n")
	env.b.WriteString(env.indent)
	env.b.WriteString("- ")
	env.b.WriteString(prop.Date.Start.String())

	if prop.Date.End != nil {
		env.b.WriteString("\n")
		env.b.WriteString(env.indent)
		env.b.WriteString("- ")
		env.b.WriteString(prop.Date.End.String())
	}

	env.b.WriteString("\n")
}

func markdownPropCheckbox(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	if *prop.Checkbox {
		env.b.WriteString("true")
	} else {
		env.b.WriteString("false")
	}
	env.b.WriteString("\n")
}

func markdownPropURL(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	if prop.URL == nil {
		env.b.WriteString("\n")
		return
	}

	env.b.WriteString(*prop.URL)
	env.b.WriteString("\n")
}

func markdownPropRelation(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	env.b.WriteString("\n")
	for _, r := range prop.Relation {
		env.b.WriteString(env.indent)
		env.b.WriteString("- [[")
		env.b.WriteString(SimpleID(r.ID))
		env.b.WriteString("]]\n")
	}
}

func markdownPropCreatedTime(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	env.b.WriteString(prop.CreatedTime.String())
	env.b.WriteString("\n")
}

func markdownPropCreatedBy(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	env.b.WriteString(prop.CreatedBy.Name)
	env.b.WriteString("\n")
}

func markdownPropLastEditedTime(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	env.b.WriteString(prop.LastEditedTime.String())
	env.b.WriteString("\n")
}

func markdownPropLastEditedBy(env *markdownEnv, key string, prop notion.DatabasePageProperty) {
	env.b.WriteString(prop.LastEditedBy.Name)
	env.b.WriteString("\n")
}
