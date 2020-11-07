package prop

import (
	"reflect"
	"testing"

	"github.com/godbus/dbus/v5"
)

type Foo struct {
	Id    int
	Value string
}

func comparePropValue(obj dbus.BusObject, name string, want interface{}, t *testing.T) {
	r, err := obj.GetProperty("org.guelfey.DBus.Test." + name)
	if err != nil {
		t.Fatal(err)
	}
	haveValue := reflect.New(reflect.TypeOf(want)).Interface()
	dbus.Store([]interface{}{r.Value()}, haveValue)
	have := reflect.ValueOf(haveValue).Elem().Interface()
	if !reflect.DeepEqual(have, want) {
		t.Errorf("struct comparison failed: got '%v', want '%v'", have, want)
	}
}

func TestValidateStructsAsProp(t *testing.T) {
	srv, err := dbus.SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	cli, err := dbus.SessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	foo := Foo{Id: 1, Value: "First"}
	fooPtr := &Foo{Id: 1, Value: "1st"}
	foos := make([]Foo, 2)
	foos[0] = Foo{Id: 1, Value: "Ones"}
	foos[1] = Foo{Id: 2, Value: "Twos"}

	propsSpec := map[string]map[string]*Prop{
		"org.guelfey.DBus.Test": {
			"FooStruct": {
				foo,
				true,
				EmitTrue,
				nil,
			},
			"FooStructPtr": {
				&fooPtr,
				true,
				EmitTrue,
				nil,
			},
			"SliceOfFoos": {
				foos,
				true,
				EmitTrue,
				nil,
			},
		},
	}
	props := New(srv, "/org/guelfey/DBus/Test", propsSpec)

	obj := cli.Object(srv.Names()[0], "/org/guelfey/DBus/Test")
	comparePropValue(obj, "FooStruct", foo, t)
	comparePropValue(obj, "FooStructPtr", *fooPtr, t)
	comparePropValue(obj, "SliceOfFoos", foos, t)

	yoo := Foo{Id: 2, Value: "Second"}
	yooPtr := &Foo{Id: 2, Value: "2nd"}
	yoos := make([]Foo, 2)
	yoos[0] = Foo{Id: 3, Value: "Threes"}
	yoos[1] = Foo{Id: 4, Value: "Fours"}
	obj.SetProperty("org.guelfey.DBus.Test.FooStruct", dbus.MakeVariant(yoo))
	obj.SetProperty("org.guelfey.DBus.Test.FooStructPtr", dbus.MakeVariant(yooPtr))
	obj.SetProperty("org.guelfey.DBus.Test.SliceOfFoos", dbus.MakeVariant(yoos))
	comparePropValue(obj, "FooStruct", yoo, t)
	comparePropValue(obj, "FooStructPtr", *yooPtr, t)
	comparePropValue(obj, "SliceOfFoos", yoos, t)

	props.SetMust("org.guelfey.DBus.Test", "SliceOfFoos", foos)
	comparePropValue(obj, "SliceOfFoos", foos, t)

	zoo := Foo{Id: 3, Value: "Third"}
	zooPtr := &Foo{Id: 3, Value: "3th"}
	zoos := make([]Foo, 2)
	zoos[0] = Foo{Id: 5, Value: "Sevens"}
	zoos[1] = Foo{Id: 6, Value: "Sixes"}
	obj.SetProperty("org.guelfey.DBus.Test.FooStruct", dbus.MakeVariant(zoo))
	obj.SetProperty("org.guelfey.DBus.Test.FooStructPtr", dbus.MakeVariant(zooPtr))
	obj.SetProperty("org.guelfey.DBus.Test.SliceOfFoos", dbus.MakeVariant(zoos))
	comparePropValue(obj, "FooStruct", zoo, t)
	comparePropValue(obj, "FooStructPtr", *zooPtr, t)
	comparePropValue(obj, "SliceOfFoos", zoos, t)
}
