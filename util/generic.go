package util

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func Must[T any](t T, err ...error) T {
	if len(err) > 0 {
		if err[0] != nil {
			panic(errors.Join(err...))
		}
	} else if tv := reflect.ValueOf(t); (tv != reflect.Value{}) {
		if verr := tv.Interface().(error); verr != nil {
			panic(verr)
		}
	}
	return t
}

func DeepCopyInto[T any](out, in *T) {
	Must(json.Unmarshal(Must(json.Marshal(in)), out))
}

func DeepCopy[T any](in *T) *T {
	out := new(T)
	DeepCopyInto(out, in)
	return out
}

func ProtoClone[T proto.Message](msg T) T {
	return proto.Clone(msg).(T)
}

func NewMessage[T proto.Message]() T {
	var t T
	return t.ProtoReflect().New().Interface().(T)
}

func FieldByName[T proto.Message](name string) protoreflect.FieldDescriptor {
	var t T
	fields := t.ProtoReflect().Descriptor().Fields()
	for i, l := 0, fields.Len(); i < l; i++ {
		field := fields.Get(i)
		if strings.EqualFold(string(field.Name()), name) {
			return field
		}
	}
	return nil
}

func FieldIndexByName[T proto.Message](name string) int {
	var t T
	fields := t.ProtoReflect().Descriptor().Fields()
	for i, l := 0, fields.Len(); i < l; i++ {
		field := fields.Get(i)
		if strings.EqualFold(string(field.Name()), name) {
			return i
		}
	}
	return -1
}
