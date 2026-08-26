package golang

import "go/types"

func interfaceShapeKnown(iface *types.Interface, seen map[types.Type]bool) bool {
	if !iface.IsMethodSet() {
		return false
	}
	if seen[iface] {
		return true
	}
	seen[iface] = true
	for index := 0; index < iface.NumEmbeddeds(); index++ {
		embedded, ok := types.Unalias(iface.EmbeddedType(index)).Underlying().(*types.Interface)
		if !ok || !interfaceShapeKnown(embedded, seen) {
			return false
		}
	}
	return true
}

func embeddedMethodSetsKnown(value types.Type, seen map[types.Type]bool) bool {
	value = types.Unalias(value)
	if seen[value] {
		return true
	}
	seen[value] = true
	if pointer, ok := value.(*types.Pointer); ok {
		return embeddedMethodSetsKnown(pointer.Elem(), seen)
	}
	if value.Underlying() == types.Typ[types.Invalid] {
		return false
	}
	structure, ok := value.Underlying().(*types.Struct)
	if !ok {
		return true
	}
	for index := 0; index < structure.NumFields(); index++ {
		field := structure.Field(index)
		if field.Embedded() && !embeddedMethodSetsKnown(field.Type(), seen) {
			return false
		}
	}
	return true
}

// Nominal identity does not require a named type's fields to be known. Structural
// signature components do: go/types treats invalid types permissively, which
// must never turn an unavailable imported type into proof of compatibility.
func signatureTypeKnown(value types.Type) bool {
	if value == nil {
		return false
	}
	value = types.Unalias(value)
	switch typed := value.(type) {
	case *types.Basic:
		return typed.Kind() != types.Invalid
	case *types.Named:
		return namedIdentityKnown(typed)
	case *types.Signature:
		return typed.TypeParams().Len() == 0 && tupleTypesKnown(typed.Params()) && tupleTypesKnown(typed.Results())
	case *types.TypeParam, *types.Union:
		return false
	default:
		return compoundIdentityKnown(value)
	}
}

func namedIdentityKnown(named *types.Named) bool {
	if named.Underlying() == types.Typ[types.Invalid] {
		return false
	}
	if named.TypeParams().Len() > 0 && named.TypeArgs().Len() == 0 {
		return false
	}
	for index := 0; index < named.TypeArgs().Len(); index++ {
		if !signatureTypeKnown(named.TypeArgs().At(index)) {
			return false
		}
	}
	return true
}

func compoundIdentityKnown(value types.Type) bool {
	switch typed := value.(type) {
	case *types.Pointer:
		return signatureTypeKnown(typed.Elem())
	case *types.Slice:
		return signatureTypeKnown(typed.Elem())
	case *types.Array:
		return typed.Len() >= 0 && signatureTypeKnown(typed.Elem())
	case *types.Map:
		return signatureTypeKnown(typed.Key()) && signatureTypeKnown(typed.Elem())
	case *types.Chan:
		return signatureTypeKnown(typed.Elem())
	case *types.Struct:
		return structTypesKnown(typed)
	case *types.Interface:
		return interfaceTypesKnown(typed)
	default:
		return false
	}
}

func tupleTypesKnown(tuple *types.Tuple) bool {
	for index := 0; index < tuple.Len(); index++ {
		if !signatureTypeKnown(tuple.At(index).Type()) {
			return false
		}
	}
	return true
}

func structTypesKnown(structure *types.Struct) bool {
	for index := 0; index < structure.NumFields(); index++ {
		if !signatureTypeKnown(structure.Field(index).Type()) {
			return false
		}
	}
	return true
}

func interfaceTypesKnown(iface *types.Interface) bool {
	if !interfaceShapeKnown(iface, map[types.Type]bool{}) {
		return false
	}
	for index := 0; index < iface.NumMethods(); index++ {
		if !signatureTypeKnown(iface.Method(index).Type()) {
			return false
		}
	}
	return true
}
