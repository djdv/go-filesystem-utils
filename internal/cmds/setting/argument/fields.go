package argument

import (
	"context"
	"reflect"

	"github.com/djdv/go-filesystem-utils/internal/cmds/setting/runtime"
)

func expandEmbedded(ctx context.Context, fields runtime.SettingsFields) runtime.SettingsFields {
	out := make(chan reflect.StructField, cap(fields))
	go func() {
		defer close(out)
		for field := range fields {
			if !field.Anonymous ||
				field.Type.Kind() != reflect.Struct {
				select {
				case out <- field:
					continue
				case <-ctx.Done():
					return
				}
			}
			for field := range expandField(ctx, field) {
				select {
				case out <- field:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

func expandField(ctx context.Context, field reflect.StructField) runtime.SettingsFields {
	var (
		embeddedFields = runtime.MustGenerateFields(ctx, field.Type)
		prefixedFields = prefixIndex(ctx, field.Index, embeddedFields)
	)
	return expandEmbedded(ctx, prefixedFields)
}

func prefixIndex(ctx context.Context, prefix []int, fields runtime.SettingsFields) runtime.SettingsFields {
	prefixed := make(chan reflect.StructField, cap(fields))
	go func() {
		defer close(prefixed)
		for field := range fields {
			field.Index = append(prefix, field.Index...)
			select {
			case prefixed <- field:
			case <-ctx.Done():
				return
			}
		}
	}()
	return prefixed
}
