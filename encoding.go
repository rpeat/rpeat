package rpeat

// custom struct tags
// https://sosedoff.com/2016/07/16/golang-struct-tags.html

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

var _ = strings.Split

type MapStringString map[string]string

func (m MapStringString) MarshalXML(e *xml.Encoder, start xml.StartElement) error {

	tokens := []xml.Token{start}

	for key, value := range m {
		t := xml.StartElement{Name: xml.Name{Space: "", Local: key}}
		tokens = append(tokens, t, xml.CharData(value), xml.EndElement{Name: t.Name})
	}

	tokens = append(tokens, xml.EndElement{Name: start.Name})

	for _, t := range tokens {
		err := e.EncodeToken(t)
		if err != nil {
			return err
		}
	}
	return e.Flush()
}

type xmlMapStringString struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

func (m *MapStringString) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	fmt.Printf("inside MapStringString")
	*m = MapStringString{}
	for {
		var e xmlMapStringString

		err := d.Decode(&e)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		(*m)[e.XMLName.Local] = e.Value
	}
	return nil
}
func (env Env) MarshalXML(e *xml.Encoder, start xml.StartElement) error {

	tokens := []xml.Token{start}

	for key, value := range env {
		t := xml.StartElement{Name: xml.Name{Space: "", Local: "Env"}}
		keyValue := fmt.Sprintf("%s=%s", key, value)
		tokens = append(tokens, t, xml.CharData(keyValue), xml.EndElement{Name: t.Name})
	}

	/* using <Env name="KEY">VALUE</Env> style
	   for key, value := range env {
	       attr := xml.Attr{Name:xml.Name{"","name"}, Value:key}
	       t := xml.StartElement{Name: xml.Name{"", "Env"}, Attr: []xml.Attr{attr}}
	       tokens = append(tokens, t, xml.CharData(value), xml.EndElement{t.Name})
	   }
	*/
	tokens = append(tokens, xml.EndElement{Name: start.Name})

	for _, t := range tokens {
		err := e.EncodeToken(t)
		if err != nil {
			return err
		}
	}
	return e.Flush()
}

type xmlEnv struct {
	XMLName xml.Name
	Data    string `xml:",chardata"`
}

// https://stackoverflow.com/questions/41454603/go-xml-unmarshal-array
// https://blog.andreiavram.ro/unmarshal-json-xml-golang-structure-slice/

// var EnvPair string
// var EnvList []EnvPair
func (m *EnvList) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	for {
		//d.Strict = false
		tok, err := d.Token()
		switch tok.(type) {
		case xml.CharData:
			kv := string(tok.(xml.CharData))
			*m = append(*m, kv)
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
	}
	return nil
}

// Dependencies (aka []Dependency)
// Process each <Dependency></Dependency>
func (s JobTrigger) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	tokens := []xml.Token{start}

	/*
	   <Dependency>
	     <Dependencies>
	       <JobTrigger>
	         <NameOrUUID>MySQL Server</NameOrUUID>
	         <Trigger>running</Trigger>
	       </JobTrigger>
	       <JobTrigger>
	         <NameOrUUID>Redis Server</NameOrUUID>
	         <Trigger>running</Trigger>
	       </JobTrigger>
	       </Dependencies>
	       <Action>start</Action>
	       <Condition>any</Condition>
	       <Delay>10s</Delay>
	   </Dependency>
	*/

	for key, value := range s {
		jobtrigger := xml.StartElement{Name: xml.Name{Space: "", Local: "JobTrigger"}}
		jobuuidelem := xml.StartElement{Name: xml.Name{Space: "", Local: "NameOrUUID"}}
		jobuuid := xml.CharData(key)
		triggerelem := xml.StartElement{Name: xml.Name{Space: "", Local: "Trigger"}}
		trigger := xml.CharData(value)
		tokens = append(tokens,
			jobtrigger,
			jobuuidelem, jobuuid, xml.EndElement{Name: jobuuidelem.Name},
			triggerelem, trigger, xml.EndElement{Name: triggerelem.Name},
			xml.EndElement{Name: jobtrigger.Name})
	}

	tokens = append(tokens, xml.EndElement{Name: start.Name})

	for _, t := range tokens {
		err := e.EncodeToken(t)
		if err != nil {
			return err
		}
	}

	return e.Flush()
}

// https://stackoverflow.com/questions/33557401/unmarshal-nested-xml-with-go
// https://play.golang.org/p/l8qdPXy2KR
type xmlJobTrigger struct {
	XMLName    xml.Name `xml:"JobTrigger"`
	NameOrUUID string   `xml:"NameOrUUID"`
	Trigger    string   `xml:"Trigger"`
}

func (m *JobTrigger) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	*m = JobTrigger{}
	for {
		var e xmlJobTrigger

		err := d.Decode(&e)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		(*m)[e.NameOrUUID] = e.Trigger
	}
	return nil
}

// Permission
func (p Permission) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	tokens := []xml.Token{start}
	for key, value := range p {
		if len(value) == 0 {
			continue
		}
		t := xml.StartElement{Name: xml.Name{Space: "", Local: key}}
		tokens = append(tokens, t)
		for _, u := range value {
			user := xml.StartElement{Name: xml.Name{Space: "", Local: "User"}}
			tokens = append(tokens, user, xml.CharData(u), xml.EndElement{Name: user.Name})
		}
		tokens = append(tokens, xml.EndElement{Name: t.Name})
	}

	tokens = append(tokens, xml.EndElement{Name: start.Name})

	for _, t := range tokens {
		err := e.EncodeToken(t)
		if err != nil {
			return err
		}
	}
	return e.Flush()
}

type xmlPermission struct {
	XMLName xml.Name
	Action  []string `xml:"User"`
}

func (p *Permission) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	*p = Permission{}
	for {
		var e xmlPermission

		err := d.Decode(&e)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}

		(*p)[e.XMLName.Local] = e.Action
	}
	return nil
}

// Email

// Exception handling
//https://maori.geek.nz/golang-raise-error-if-unknown-field-in-json-with-exceptions-2b0caddecd1

func replaceHTML(x []byte) []byte {
	s := string(x)
	s = strings.ReplaceAll(s, "&#34;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&#10;", "\n")
	s = strings.ReplaceAll(s, "&#xA;", "\n")
	return []byte(s)
}

func replaceEntities(x []byte) []byte {
	s := string(x)
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<(", "&lt;(")
	return []byte(s)
}
