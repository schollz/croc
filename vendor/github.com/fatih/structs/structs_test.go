package structs

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestMapNonStruct(t *testing.T) {
	foo := []string{"foo"}

	defer func() {
		err := recover()
		if err == nil {
			t.Error("Passing a non struct into Map should panic")
		}
	}()

	// this should panic. We are going to recover and and test it
	_ = Map(foo)
}

func TestStructIndexes(t *testing.T) {
	type C struct {
		something int
		Props     map[string]interface{}
	}

	defer func() {
		err := recover()
		if err != nil {
			fmt.Printf("err %+v\n", err)
			t.Error("Using mixed indexes should not panic")
		}
	}()

	// They should not panic
	_ = Map(&C{})
	_ = Fields(&C{})
	_ = Values(&C{})
	_ = IsZero(&C{})
	_ = HasZero(&C{})
}

func TestMap(t *testing.T) {
	var T = struct {
		A string
		B int
		C bool
	}{
		A: "a-value",
		B: 2,
		C: true,
	}

	a := Map(T)

	if typ := reflect.TypeOf(a).Kind(); typ != reflect.Map {
		t.Errorf("Map should return a map type, got: %v", typ)
	}

	// we have three fields
	if len(a) != 3 {
		t.Errorf("Map should return a map of len 3, got: %d", len(a))
	}

	inMap := func(val interface{}) bool {
		for _, v := range a {
			if reflect.DeepEqual(v, val) {
				return true
			}
		}

		return false
	}

	for _, val := range []interface{}{"a-value", 2, true} {
		if !inMap(val) {
			t.Errorf("Map should have the value %v", val)
		}
	}

}

func TestMap_Tag(t *testing.T) {
	var T = struct {
		A string `structs:"x"`
		B int    `structs:"y"`
		C bool   `structs:"z"`
	}{
		A: "a-value",
		B: 2,
		C: true,
	}

	a := Map(T)

	inMap := func(key interface{}) bool {
		for k := range a {
			if reflect.DeepEqual(k, key) {
				return true
			}
		}
		return false
	}

	for _, key := range []string{"x", "y", "z"} {
		if !inMap(key) {
			t.Errorf("Map should have the key %v", key)
		}
	}

}

func TestMap_CustomTag(t *testing.T) {
	var T = struct {
		A string `json:"x"`
		B int    `json:"y"`
		C bool   `json:"z"`
		D struct {
			E string `json:"jkl"`
		} `json:"nested"`
	}{
		A: "a-value",
		B: 2,
		C: true,
	}
	T.D.E = "e-value"

	s := New(T)
	s.TagName = "json"

	a := s.Map()

	inMap := func(key interface{}) bool {
		for k := range a {
			if reflect.DeepEqual(k, key) {
				return true
			}
		}
		return false
	}

	for _, key := range []string{"x", "y", "z"} {
		if !inMap(key) {
			t.Errorf("Map should have the key %v", key)
		}
	}

	nested, ok := a["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("Map should contain the D field that is tagged as 'nested'")
	}

	e, ok := nested["jkl"].(string)
	if !ok {
		t.Fatalf("Map should contain the D.E field that is tagged as 'jkl'")
	}

	if e != "e-value" {
		t.Errorf("D.E field should be equal to 'e-value', got: '%v'", e)
	}

}

func TestMap_MultipleCustomTag(t *testing.T) {
	var A = struct {
		X string `aa:"ax"`
	}{"a_value"}

	aStruct := New(A)
	aStruct.TagName = "aa"

	var B = struct {
		X string `bb:"bx"`
	}{"b_value"}

	bStruct := New(B)
	bStruct.TagName = "bb"

	a, b := aStruct.Map(), bStruct.Map()
	if !reflect.DeepEqual(a, map[string]interface{}{"ax": "a_value"}) {
		t.Error("Map should have field ax with value a_value")
	}

	if !reflect.DeepEqual(b, map[string]interface{}{"bx": "b_value"}) {
		t.Error("Map should have field bx with value b_value")
	}
}

func TestMap_OmitEmpty(t *testing.T) {
	type A struct {
		Name  string
		Value string    `structs:",omitempty"`
		Time  time.Time `structs:",omitempty"`
	}
	a := A{}

	m := Map(a)

	_, ok := m["Value"].(map[string]interface{})
	if ok {
		t.Error("Map should not contain the Value field that is tagged as omitempty")
	}

	_, ok = m["Time"].(map[string]interface{})
	if ok {
		t.Error("Map should not contain the Time field that is tagged as omitempty")
	}
}

func TestMap_OmitNested(t *testing.T) {
	type A struct {
		Name  string
		Value string
		Time  time.Time `structs:",omitnested"`
	}
	a := A{Time: time.Now()}

	type B struct {
		Desc string
		A    A
	}
	b := &B{A: a}

	m := Map(b)

	in, ok := m["A"].(map[string]interface{})
	if !ok {
		t.Error("Map nested structs is not available in the map")
	}

	// should not happen
	if _, ok := in["Time"].(map[string]interface{}); ok {
		t.Error("Map nested struct should omit recursiving parsing of Time")
	}

	if _, ok := in["Time"].(time.Time); !ok {
		t.Error("Map nested struct should stop parsing of Time at is current value")
	}
}

func TestMap_Nested(t *testing.T) {
	type A struct {
		Name string
	}
	a := &A{Name: "example"}

	type B struct {
		A *A
	}
	b := &B{A: a}

	m := Map(b)

	if typ := reflect.TypeOf(m).Kind(); typ != reflect.Map {
		t.Errorf("Map should return a map type, got: %v", typ)
	}

	in, ok := m["A"].(map[string]interface{})
	if !ok {
		t.Error("Map nested structs is not available in the map")
	}

	if name := in["Name"].(string); name != "example" {
		t.Errorf("Map nested struct's name field should give example, got: %s", name)
	}
}

func TestMap_NestedMapWithStructValues(t *testing.T) {
	type A struct {
		Name string
	}

	type B struct {
		A map[string]*A
	}

	a := &A{Name: "example"}

	b := &B{
		A: map[string]*A{
			"example_key": a,
		},
	}

	m := Map(b)

	if typ := reflect.TypeOf(m).Kind(); typ != reflect.Map {
		t.Errorf("Map should return a map type, got: %v", typ)
	}

	in, ok := m["A"].(map[string]interface{})
	if !ok {
		t.Errorf("Nested type of map should be of type map[string]interface{}, have %T", m["A"])
	}

	example := in["example_key"].(map[string]interface{})
	if name := example["Name"].(string); name != "example" {
		t.Errorf("Map nested struct's name field should give example, got: %s", name)
	}
}

func TestMap_NestedMapWithStringValues(t *testing.T) {
	type B struct {
		Foo map[string]string
	}

	type A struct {
		B *B
	}

	b := &B{
		Foo: map[string]string{
			"example_key": "example",
		},
	}

	a := &A{B: b}

	m := Map(a)

	if typ := reflect.TypeOf(m).Kind(); typ != reflect.Map {
		t.Errorf("Map should return a map type, got: %v", typ)
	}

	in, ok := m["B"].(map[string]interface{})
	if !ok {
		t.Errorf("Nested type of map should be of type map[string]interface{}, have %T", m["B"])
	}

	foo := in["Foo"].(map[string]string)
	if name := foo["example_key"]; name != "example" {
		t.Errorf("Map nested struct's name field should give example, got: %s", name)
	}
}
func TestMap_NestedMapWithInterfaceValues(t *testing.T) {
	type B struct {
		Foo map[string]interface{}
	}

	type A struct {
		B *B
	}

	b := &B{
		Foo: map[string]interface{}{
			"example_key": "example",
		},
	}

	a := &A{B: b}

	m := Map(a)

	if typ := reflect.TypeOf(m).Kind(); typ != reflect.Map {
		t.Errorf("Map should return a map type, got: %v", typ)
	}

	in, ok := m["B"].(map[string]interface{})
	if !ok {
		t.Errorf("Nested type of map should be of type map[string]interface{}, have %T", m["B"])
	}

	foo := in["Foo"].(map[string]interface{})
	if name := foo["example_key"]; name != "example" {
		t.Errorf("Map nested struct's name field should give example, got: %s", name)
	}
}

func TestMap_NestedMapWithSliceIntValues(t *testing.T) {
	type B struct {
		Foo map[string][]int
	}

	type A struct {
		B *B
	}

	b := &B{
		Foo: map[string][]int{
			"example_key": []int{80},
		},
	}

	a := &A{B: b}

	m := Map(a)

	if typ := reflect.TypeOf(m).Kind(); typ != reflect.Map {
		t.Errorf("Map should return a map type, got: %v", typ)
	}

	in, ok := m["B"].(map[string]interface{})
	if !ok {
		t.Errorf("Nested type of map should be of type map[string]interface{}, have %T", m["B"])
	}

	foo := in["Foo"].(map[string][]int)
	if name := foo["example_key"]; name[0] != 80 {
		t.Errorf("Map nested struct's name field should give example, got: %s", name)
	}
}

func TestMap_NestedMapWithSliceStructValues(t *testing.T) {
	type address struct {
		Country string `structs:"country"`
	}

	type B struct {
		Foo map[string][]address
	}

	type A struct {
		B *B
	}

	b := &B{
		Foo: map[string][]address{
			"example_key": []address{
				{Country: "Turkey"},
			},
		},
	}

	a := &A{B: b}
	m := Map(a)

	if typ := reflect.TypeOf(m).Kind(); typ != reflect.Map {
		t.Errorf("Map should return a map type, got: %v", typ)
	}

	in, ok := m["B"].(map[string]interface{})
	if !ok {
		t.Errorf("Nested type of map should be of type map[string]interface{}, have %T", m["B"])
	}

	foo := in["Foo"].(map[string]interface{})

	addresses := foo["example_key"].([]interface{})

	addr, ok := addresses[0].(map[string]interface{})
	if !ok {
		t.Errorf("Nested type of map should be of type map[string]interface{}, have %T", m["B"])
	}

	if _, exists := addr["country"]; !exists {
		t.Errorf("Expecting country, but found Country")
	}
}

func TestMap_NestedSliceWithStructValues(t *testing.T) {
	type address struct {
		Country string `structs:"customCountryName"`
	}

	type person struct {
		Name      string    `structs:"name"`
		Addresses []address `structs:"addresses"`
	}

	p := person{
		Name: "test",
		Addresses: []address{
			address{Country: "England"},
			address{Country: "Italy"},
		},
	}
	mp := Map(p)

	mpAddresses := mp["addresses"].([]interface{})
	if _, exists := mpAddresses[0].(map[string]interface{})["Country"]; exists {
		t.Errorf("Expecting customCountryName, but found Country")
	}

	if _, exists := mpAddresses[0].(map[string]interface{})["customCountryName"]; !exists {
		t.Errorf("customCountryName key not found")
	}
}

func TestMap_NestedSliceWithPointerOfStructValues(t *testing.T) {
	type address struct {
		Country string `structs:"customCountryName"`
	}

	type person struct {
		Name      string     `structs:"name"`
		Addresses []*address `structs:"addresses"`
	}

	p := person{
		Name: "test",
		Addresses: []*address{
			&address{Country: "England"},
			&address{Country: "Italy"},
		},
	}
	mp := Map(p)

	mpAddresses := mp["addresses"].([]interface{})
	if _, exists := mpAddresses[0].(map[string]interface{})["Country"]; exists {
		t.Errorf("Expecting customCountryName, but found Country")
	}

	if _, exists := mpAddresses[0].(map[string]interface{})["customCountryName"]; !exists {
		t.Errorf("customCountryName key not found")
	}
}

func TestMap_NestedSliceWithIntValues(t *testing.T) {
	type person struct {
		Name  string `structs:"name"`
		Ports []int  `structs:"ports"`
	}

	p := person{
		Name:  "test",
		Ports: []int{80},
	}
	m := Map(p)

	ports, ok := m["ports"].([]int)
	if !ok {
		t.Errorf("Nested type of map should be of type []int, have %T", m["ports"])
	}

	if ports[0] != 80 {
		t.Errorf("Map nested struct's ports field should give 80, got: %v", ports)
	}
}

func TestMap_Anonymous(t *testing.T) {
	type A struct {
		Name string
	}
	a := &A{Name: "example"}

	type B struct {
		*A
	}
	b := &B{}
	b.A = a

	m := Map(b)

	if typ := reflect.TypeOf(m).Kind(); typ != reflect.Map {
		t.Errorf("Map should return a map type, got: %v", typ)
	}

	in, ok := m["A"].(map[string]interface{})
	if !ok {
		t.Error("Embedded structs is not available in the map")
	}

	if name := in["Name"].(string); name != "example" {
		t.Errorf("Embedded A struct's Name field should give example, got: %s", name)
	}
}

func TestMap_Flatnested(t *testing.T) {
	type A struct {
		Name string
	}
	a := A{Name: "example"}

	type B struct {
		A `structs:",flatten"`
		C int
	}
	b := &B{C: 123}
	b.A = a

	m := Map(b)

	_, ok := m["A"].(map[string]interface{})
	if ok {
		t.Error("Embedded A struct with tag flatten has to be flat in the map")
	}

	expectedMap := map[string]interface{}{"Name": "example", "C": 123}
	if !reflect.DeepEqual(m, expectedMap) {
		t.Errorf("The exprected map %+v does't correspond to %+v", expectedMap, m)
	}

}

func TestMap_FlatnestedOverwrite(t *testing.T) {
	type A struct {
		Name string
	}
	a := A{Name: "example"}

	type B struct {
		A    `structs:",flatten"`
		Name string
		C    int
	}
	b := &B{C: 123, Name: "bName"}
	b.A = a

	m := Map(b)

	_, ok := m["A"].(map[string]interface{})
	if ok {
		t.Error("Embedded A struct with tag flatten has to be flat in the map")
	}

	expectedMap := map[string]interface{}{"Name": "bName", "C": 123}
	if !reflect.DeepEqual(m, expectedMap) {
		t.Errorf("The exprected map %+v does't correspond to %+v", expectedMap, m)
	}
}

func TestMap_TimeField(t *testing.T) {
	type A struct {
		CreatedAt time.Time
	}

	a := &A{CreatedAt: time.Now().UTC()}
	m := Map(a)

	_, ok := m["CreatedAt"].(time.Time)
	if !ok {
		t.Error("Time field must be final")
	}
}

func TestFillMap(t *testing.T) {
	var T = struct {
		A string
		B int
		C bool
	}{
		A: "a-value",
		B: 2,
		C: true,
	}

	a := make(map[string]interface{}, 0)
	FillMap(T, a)

	// we have three fields
	if len(a) != 3 {
		t.Errorf("FillMap should fill a map of len 3, got: %d", len(a))
	}

	inMap := func(val interface{}) bool {
		for _, v := range a {
			if reflect.DeepEqual(v, val) {
				return true
			}
		}

		return false
	}

	for _, val := range []interface{}{"a-value", 2, true} {
		if !inMap(val) {
			t.Errorf("FillMap should have the value %v", val)
		}
	}
}

func TestFillMap_Nil(t *testing.T) {
	var T = struct {
		A string
		B int
		C bool
	}{
		A: "a-value",
		B: 2,
		C: true,
	}

	defer func() {
		err := recover()
		if err != nil {
			t.Error("FillMap should not panic if a nil map is passed")
		}
	}()

	// nil should no
	FillMap(T, nil)
}
func TestStruct(t *testing.T) {
	var T = struct{}{}

	if !IsStruct(T) {
		t.Errorf("T should be a struct, got: %T", T)
	}

	if !IsStruct(&T) {
		t.Errorf("T should be a struct, got: %T", T)
	}

}

func TestValues(t *testing.T) {
	var T = struct {
		A string
		B int
		C bool
	}{
		A: "a-value",
		B: 2,
		C: true,
	}

	s := Values(T)

	if typ := reflect.TypeOf(s).Kind(); typ != reflect.Slice {
		t.Errorf("Values should return a slice type, got: %v", typ)
	}

	inSlice := func(val interface{}) bool {
		for _, v := range s {
			if reflect.DeepEqual(v, val) {
				return true
			}
		}
		return false
	}

	for _, val := range []interface{}{"a-value", 2, true} {
		if !inSlice(val) {
			t.Errorf("Values should have the value %v", val)
		}
	}
}

func TestValues_OmitEmpty(t *testing.T) {
	type A struct {
		Name  string
		Value int `structs:",omitempty"`
	}

	a := A{Name: "example"}
	s := Values(a)

	if len(s) != 1 {
		t.Errorf("Values of omitted empty fields should be not counted")
	}

	if s[0].(string) != "example" {
		t.Errorf("Values of omitted empty fields should left the value example")
	}
}

func TestValues_OmitNested(t *testing.T) {
	type A struct {
		Name  string
		Value int
	}

	a := A{
		Name:  "example",
		Value: 123,
	}

	type B struct {
		A A `structs:",omitnested"`
		C int
	}
	b := &B{A: a, C: 123}

	s := Values(b)

	if len(s) != 2 {
		t.Errorf("Values of omitted nested struct should be not counted")
	}

	inSlice := func(val interface{}) bool {
		for _, v := range s {
			if reflect.DeepEqual(v, val) {
				return true
			}
		}
		return false
	}

	for _, val := range []interface{}{123, a} {
		if !inSlice(val) {
			t.Errorf("Values should have the value %v", val)
		}
	}
}

func TestValues_Nested(t *testing.T) {
	type A struct {
		Name string
	}
	a := A{Name: "example"}

	type B struct {
		A A
		C int
	}
	b := &B{A: a, C: 123}

	s := Values(b)

	inSlice := func(val interface{}) bool {
		for _, v := range s {
			if reflect.DeepEqual(v, val) {
				return true
			}
		}
		return false
	}

	for _, val := range []interface{}{"example", 123} {
		if !inSlice(val) {
			t.Errorf("Values should have the value %v", val)
		}
	}
}

func TestValues_Anonymous(t *testing.T) {
	type A struct {
		Name string
	}
	a := A{Name: "example"}

	type B struct {
		A
		C int
	}
	b := &B{C: 123}
	b.A = a

	s := Values(b)

	inSlice := func(val interface{}) bool {
		for _, v := range s {
			if reflect.DeepEqual(v, val) {
				return true
			}
		}
		return false
	}

	for _, val := range []interface{}{"example", 123} {
		if !inSlice(val) {
			t.Errorf("Values should have the value %v", val)
		}
	}
}

func TestNames(t *testing.T) {
	var T = struct {
		A string
		B int
		C bool
	}{
		A: "a-value",
		B: 2,
		C: true,
	}

	s := Names(T)

	if len(s) != 3 {
		t.Errorf("Names should return a slice of len 3, got: %d", len(s))
	}

	inSlice := func(val string) bool {
		for _, v := range s {
			if reflect.DeepEqual(v, val) {
				return true
			}
		}
		return false
	}

	for _, val := range []string{"A", "B", "C"} {
		if !inSlice(val) {
			t.Errorf("Names should have the value %v", val)
		}
	}
}

func TestFields(t *testing.T) {
	var T = struct {
		A string
		B int
		C bool
	}{
		A: "a-value",
		B: 2,
		C: true,
	}

	s := Fields(T)

	if len(s) != 3 {
		t.Errorf("Fields should return a slice of len 3, got: %d", len(s))
	}

	inSlice := func(val string) bool {
		for _, v := range s {
			if reflect.DeepEqual(v.Name(), val) {
				return true
			}
		}
		return false
	}

	for _, val := range []string{"A", "B", "C"} {
		if !inSlice(val) {
			t.Errorf("Fields should have the value %v", val)
		}
	}
}

func TestFields_OmitNested(t *testing.T) {
	type A struct {
		Name    string
		Enabled bool
	}
	a := A{Name: "example"}

	type B struct {
		A      A
		C      int
		Value  string `structs:"-"`
		Number int
	}
	b := &B{A: a, C: 123}

	s := Fields(b)

	if len(s) != 3 {
		t.Errorf("Fields should omit nested struct. Expecting 2 got: %d", len(s))
	}

	inSlice := func(val interface{}) bool {
		for _, v := range s {
			if reflect.DeepEqual(v.Name(), val) {
				return true
			}
		}
		return false
	}

	for _, val := range []interface{}{"A", "C"} {
		if !inSlice(val) {
			t.Errorf("Fields should have the value %v", val)
		}
	}
}

func TestFields_Anonymous(t *testing.T) {
	type A struct {
		Name string
	}
	a := A{Name: "example"}

	type B struct {
		A
		C int
	}
	b := &B{C: 123}
	b.A = a

	s := Fields(b)

	inSlice := func(val interface{}) bool {
		for _, v := range s {
			if reflect.DeepEqual(v.Name(), val) {
				return true
			}
		}
		return false
	}

	for _, val := range []interface{}{"A", "C"} {
		if !inSlice(val) {
			t.Errorf("Fields should have the value %v", val)
		}
	}
}

func TestIsZero(t *testing.T) {
	var T = struct {
		A string
		B int
		C bool `structs:"-"`
		D []string
	}{}

	ok := IsZero(T)
	if !ok {
		t.Error("IsZero should return true because none of the fields are initialized.")
	}

	var X = struct {
		A string
		F *bool
	}{
		A: "a-value",
	}

	ok = IsZero(X)
	if ok {
		t.Error("IsZero should return false because A is initialized")
	}

	var Y = struct {
		A string
		B int
	}{
		A: "a-value",
		B: 123,
	}

	ok = IsZero(Y)
	if ok {
		t.Error("IsZero should return false because A and B is initialized")
	}
}

func TestIsZero_OmitNested(t *testing.T) {
	type A struct {
		Name string
		D    string
	}
	a := A{Name: "example"}

	type B struct {
		A A `structs:",omitnested"`
		C int
	}
	b := &B{A: a, C: 123}

	ok := IsZero(b)
	if ok {
		t.Error("IsZero should return false because A, B and C are initialized")
	}

	aZero := A{}
	bZero := &B{A: aZero}

	ok = IsZero(bZero)
	if !ok {
		t.Error("IsZero should return true because neither A nor B is initialized")
	}

}

func TestIsZero_Nested(t *testing.T) {
	type A struct {
		Name string
		D    string
	}
	a := A{Name: "example"}

	type B struct {
		A A
		C int
	}
	b := &B{A: a, C: 123}

	ok := IsZero(b)
	if ok {
		t.Error("IsZero should return false because A, B and C are initialized")
	}

	aZero := A{}
	bZero := &B{A: aZero}

	ok = IsZero(bZero)
	if !ok {
		t.Error("IsZero should return true because neither A nor B is initialized")
	}

}

func TestIsZero_Anonymous(t *testing.T) {
	type A struct {
		Name string
		D    string
	}
	a := A{Name: "example"}

	type B struct {
		A
		C int
	}
	b := &B{C: 123}
	b.A = a

	ok := IsZero(b)
	if ok {
		t.Error("IsZero should return false because A, B and C are initialized")
	}

	aZero := A{}
	bZero := &B{}
	bZero.A = aZero

	ok = IsZero(bZero)
	if !ok {
		t.Error("IsZero should return true because neither A nor B is initialized")
	}
}

func TestHasZero(t *testing.T) {
	var T = struct {
		A string
		B int
		C bool `structs:"-"`
		D []string
	}{
		A: "a-value",
		B: 2,
	}

	ok := HasZero(T)
	if !ok {
		t.Error("HasZero should return true because A and B are initialized.")
	}

	var X = struct {
		A string
		F *bool
	}{
		A: "a-value",
	}

	ok = HasZero(X)
	if !ok {
		t.Error("HasZero should return true because A is initialized")
	}

	var Y = struct {
		A string
		B int
	}{
		A: "a-value",
		B: 123,
	}

	ok = HasZero(Y)
	if ok {
		t.Error("HasZero should return false because A and B is initialized")
	}
}

func TestHasZero_OmitNested(t *testing.T) {
	type A struct {
		Name string
		D    string
	}
	a := A{Name: "example"}

	type B struct {
		A A `structs:",omitnested"`
		C int
	}
	b := &B{A: a, C: 123}

	// Because the Field A inside B is omitted  HasZero should return false
	// because it will stop iterating deeper andnot going to lookup for D
	ok := HasZero(b)
	if ok {
		t.Error("HasZero should return false because A and C are initialized")
	}
}

func TestHasZero_Nested(t *testing.T) {
	type A struct {
		Name string
		D    string
	}
	a := A{Name: "example"}

	type B struct {
		A A
		C int
	}
	b := &B{A: a, C: 123}

	ok := HasZero(b)
	if !ok {
		t.Error("HasZero should return true because D is not initialized")
	}
}

func TestHasZero_Anonymous(t *testing.T) {
	type A struct {
		Name string
		D    string
	}
	a := A{Name: "example"}

	type B struct {
		A
		C int
	}
	b := &B{C: 123}
	b.A = a

	ok := HasZero(b)
	if !ok {
		t.Error("HasZero should return false because D is not initialized")
	}
}

func TestName(t *testing.T) {
	type Foo struct {
		A string
		B bool
	}
	f := &Foo{}

	n := Name(f)
	if n != "Foo" {
		t.Errorf("Name should return Foo, got: %s", n)
	}

	unnamed := struct{ Name string }{Name: "Cihangir"}
	m := Name(unnamed)
	if m != "" {
		t.Errorf("Name should return empty string for unnamed struct, got: %s", n)
	}

	defer func() {
		err := recover()
		if err == nil {
			t.Error("Name should panic if a non struct is passed")
		}
	}()

	Name([]string{})
}

func TestNestedNilPointer(t *testing.T) {
	type Collar struct {
		Engraving string
	}

	type Dog struct {
		Name   string
		Collar *Collar
	}

	type Person struct {
		Name string
		Dog  *Dog
	}

	person := &Person{
		Name: "John",
	}

	personWithDog := &Person{
		Name: "Ron",
		Dog: &Dog{
			Name: "Rover",
		},
	}

	personWithDogWithCollar := &Person{
		Name: "Kon",
		Dog: &Dog{
			Name: "Ruffles",
			Collar: &Collar{
				Engraving: "If lost, call Kon",
			},
		},
	}

	defer func() {
		err := recover()
		if err != nil {
			fmt.Printf("err %+v\n", err)
			t.Error("Internal nil pointer should not panic")
		}
	}()

	_ = Map(person)                  // Panics
	_ = Map(personWithDog)           // Panics
	_ = Map(personWithDogWithCollar) // Doesn't panic
}

func TestSetValueOnNestedField(t *testing.T) {
	type Base struct {
		ID int
	}

	type User struct {
		Base
		Name string
	}

	u := User{}
	s := New(&u)
	f := s.Field("Base").Field("ID")
	err := f.Set(10)
	if err != nil {
		t.Errorf("Error %v", err)
	}
	if f.Value().(int) != 10 {
		t.Errorf("Value should be equal to 10, got %v", f.Value())
	}
}

type Person struct {
	Name string
	Age  int
}

func (p *Person) String() string {
	return fmt.Sprintf("%s(%d)", p.Name, p.Age)
}

func TestTagWithStringOption(t *testing.T) {

	type Address struct {
		Country string  `json:"country"`
		Person  *Person `json:"person,string"`
	}

	person := &Person{
		Name: "John",
		Age:  23,
	}

	address := &Address{
		Country: "EU",
		Person:  person,
	}

	defer func() {
		err := recover()
		if err != nil {
			fmt.Printf("err %+v\n", err)
			t.Error("Internal nil pointer should not panic")
		}
	}()

	s := New(address)

	s.TagName = "json"
	m := s.Map()

	if m["person"] != person.String() {
		t.Errorf("Value for field person should be %s, got: %s", person.String(), m["person"])
	}

	vs := s.Values()
	if vs[1] != person.String() {
		t.Errorf("Value for 2nd field (person) should be %T, got: %T", person.String(), vs[1])
	}
}

type Animal struct {
	Name string
	Age  int
}

type Dog struct {
	Animal *Animal `json:"animal,string"`
}

func TestNonStringerTagWithStringOption(t *testing.T) {
	a := &Animal{
		Name: "Fluff",
		Age:  4,
	}

	d := &Dog{
		Animal: a,
	}

	defer func() {
		err := recover()
		if err != nil {
			fmt.Printf("err %+v\n", err)
			t.Error("Internal nil pointer should not panic")
		}
	}()

	s := New(d)

	s.TagName = "json"
	m := s.Map()

	if _, exists := m["animal"]; exists {
		t.Errorf("Value for field Animal should not exist")
	}
}

func TestMap_InterfaceValue(t *testing.T) {
	type TestStruct struct {
		A interface{}
	}

	expected := []byte("test value")

	a := TestStruct{A: expected}
	s := Map(a)
	if !reflect.DeepEqual(s["A"], expected) {
		t.Errorf("Value does not match expected: %q != %q", s["A"], expected)
	}
}

func TestPointer2Pointer(t *testing.T) {
	defer func() {
		err := recover()
		if err != nil {
			fmt.Printf("err %+v\n", err)
			t.Error("Internal nil pointer should not panic")
		}
	}()
	a := &Animal{
		Name: "Fluff",
		Age:  4,
	}
	_ = Map(&a)

	b := &a
	_ = Map(&b)

	c := &b
	_ = Map(&c)
}

func TestMap_InterfaceTypeWithMapValue(t *testing.T) {
	type A struct {
		Name    string      `structs:"name"`
		Ip      string      `structs:"ip"`
		Query   string      `structs:"query"`
		Payload interface{} `structs:"payload"`
	}

	a := A{
		Name:    "test",
		Ip:      "127.0.0.1",
		Query:   "",
		Payload: map[string]string{"test_param": "test_param"},
	}

	defer func() {
		err := recover()
		if err != nil {
			t.Error("Converting Map with an interface{} type with map value should not panic")
		}
	}()

	_ = Map(a)
}
