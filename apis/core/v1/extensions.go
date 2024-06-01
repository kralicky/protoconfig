package corev1

import (
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func NewRevision(revision int64, maybeTimestamp ...time.Time) *Revision {
	return &Revision{
		Revision: &revision,
		Timestamp: func() *timestamppb.Timestamp {
			if len(maybeTimestamp) > 0 && !maybeTimestamp[0].IsZero() {
				return timestamppb.New(maybeTimestamp[0])
			}
			return nil
		}(),
	}
}

// Set sets the revision to the given value, and clears the timestamp.
func (r *Revision) Set(revision int64) {
	if r == nil {
		panic("revision is nil")
	}
	if r.Revision == nil {
		r.Revision = &revision
	} else {
		*r.Revision = revision
		r.Timestamp = nil
	}
}

func MaskedFields[T proto.Message]() []protoreflect.FieldDescriptor {
	var maskedFields []protoreflect.FieldDescriptor
	var t T
	fields := t.ProtoReflect().Descriptor().Fields()
	for i, l := 0, fields.Len(); i < l; i++ {
		field := fields.Get(i)
		if proto.HasExtension(field.Options(), E_Masked) {
			maskedFields = append(maskedFields, field)
		}
	}
	return maskedFields
}
