package bimap

import (
	"reflect"
	"testing"
)

const key = "key"
const value = "value"

func TestNewBiMap(t *testing.T) {
	actual := NewBiMap[string, string]()
	expected := &BiMap[string, string]{forward: make(map[string]string), inverse: make(map[string]string)}
	if !reflect.DeepEqual(expected, actual) {
		t.Fail()
	}
}

func TestNewBiMapFrom(t *testing.T) {
	actual := NewBiMapFromMap(map[string]string{
		key: value,
	})
	actual.Insert(key, value)

	fwdExpected := make(map[string]string)
	invExpected := make(map[string]string)
	fwdExpected[key] = value
	invExpected[value] = key
	expected := &BiMap[string, string]{forward: fwdExpected, inverse: invExpected}

	if !reflect.DeepEqual(expected, actual) {
		t.Fail()
	}
}

func TestBiMap_Insert(t *testing.T) {
	actual := NewBiMap[string, string]()
	actual.Insert(key, value)

	fwdExpected := make(map[string]string)
	invExpected := make(map[string]string)
	fwdExpected[key] = value
	invExpected[value] = key
	expected := &BiMap[string, string]{forward: fwdExpected, inverse: invExpected}

	if !reflect.DeepEqual(expected, actual) {
		t.Fail()
	}
}

func TestBiMap_Exists(t *testing.T) {
	actual := NewBiMap[string, string]()

	actual.Insert(key, value)
	if actual.Exists("ARBITARY_KEY") {
		t.Errorf("Key should not exist")
	}
	if !actual.Exists(key) {
		t.Errorf("Inserted key should exist")
	}
}

func TestBiMap_InverseExists(t *testing.T) {
	actual := NewBiMap[string, string]()

	actual.Insert(key, value)
	if actual.ExistsInverse("ARBITARY_VALUE") {
		t.Errorf("Value should not exist")
	}
	if !actual.ExistsInverse(value) {
		t.Errorf("Inserted value should exist")
	}
}

func TestBiMap_Get(t *testing.T) {
	actual := NewBiMap[string, string]()

	actual.Insert(key, value)

	actualVal, ok := actual.Get(key)
	if !ok {
		t.FailNow()
	}
	if value != actualVal {
		t.Fail()
	}

	actualVal, ok = actual.Get(value)
	if ok {
		t.FailNow()
	}
	if actualVal != "" {
		t.Fail()
	}
}

func TestBiMap_GetInverse(t *testing.T) {
	actual := NewBiMap[string, string]()

	actual.Insert(key, value)

	actualKey, ok := actual.GetInverse(value)
	if !ok {
		t.FailNow()
	}
	if key != actualKey {
		t.Fail()
	}

	actualKey, ok = actual.Get(value)
	if ok {
		t.FailNow()
	}
	if actualKey != "" {
		t.Fail()
	}
}

func TestBiMap_Size(t *testing.T) {
	actual := NewBiMap[string, string]()
	if actual.Size() != 0 {
		t.FailNow()
	}

	actual.Insert(key, value)
	if actual.Size() != 1 {
		t.Fail()
	}
}

func TestBiMap_Delete(t *testing.T) {
	actual := NewBiMap[string, string]()
	dummyKey := "DummyKey"
	dummyVal := "DummyVal"
	actual.Insert(key, value)
	actual.Insert(dummyKey, dummyVal)
	if actual.Size() != 2 {
		t.FailNow()
	}

	actual.Delete(dummyKey)

	fwdExpected := make(map[string]string)
	invExpected := make(map[string]string)
	fwdExpected[key] = value
	invExpected[value] = key

	expected := &BiMap[string, string]{forward: fwdExpected, inverse: invExpected}
	if actual.Size() != 1 {
		t.FailNow()
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fail()
	}

	actual.Delete(dummyKey)
	if actual.Size() != 1 {
		t.FailNow()
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fail()
	}
}

func TestBiMap_InverseDelete(t *testing.T) {
	actual := NewBiMap[string, string]()
	dummyKey := "DummyKey"
	dummyVal := "DummyVal"
	actual.Insert(key, value)
	actual.Insert(dummyKey, dummyVal)
	if actual.Size() != 2 {
		t.FailNow()
	}

	actual.DeleteInverse(dummyVal)

	fwdExpected := make(map[string]string)
	invExpected := make(map[string]string)
	fwdExpected[key] = value
	invExpected[value] = key

	expected := &BiMap[string, string]{forward: fwdExpected, inverse: invExpected}
	if actual.Size() != 1 {
		t.FailNow()
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fail()
	}

	actual.DeleteInverse(dummyVal)
	if actual.Size() != 1 {
		t.FailNow()
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fail()
	}
}

func TestBiMap_WithVaryingType(t *testing.T) {
	actual := NewBiMap[string, int]()
	dummyKey := "Dummy key"
	dummyVal := 3

	actual.Insert(dummyKey, dummyVal)

	res, _ := actual.Get(dummyKey)
	resVal, _ := actual.GetInverse(dummyVal)
	if dummyVal != res {
		t.Errorf("Get by string key should return integer val")
	}
	if dummyKey != resVal {
		t.Errorf("Get by integer val should return string key")
	}
}

func TestBiMap_GetForwardMap(t *testing.T) {
	actual := NewBiMap[string, int]()
	dummyKey := "Dummy key"
	dummyVal := 42

	forwardMap := make(map[string]int)
	forwardMap[dummyKey] = dummyVal

	actual.Insert(dummyKey, dummyVal)

	actualForwardMap := actual.GetForwardMap()
	if !reflect.DeepEqual(actualForwardMap, forwardMap) {
		t.Fail()
	}
}

func TestBiMap_GetInverseMap(t *testing.T) {
	actual := NewBiMap[string, int]()
	dummyKey := "Dummy key"
	dummyVal := 42

	inverseMap := make(map[int]string)
	inverseMap[dummyVal] = dummyKey

	actual.Insert(dummyKey, dummyVal)

	actualInverseMap := actual.GetInverseMap()
	if !reflect.DeepEqual(actualInverseMap, inverseMap) {
		t.Fail()
	}
}
