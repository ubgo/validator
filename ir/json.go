package ir

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// This file is the JSON codec for the sealed [TypeNode] union.
//
// Why hand-written: encoding/json cannot decode into an interface, and the
// wire format needs a "kind" discriminator on every node. Each concrete node
// marshals itself with the kind injected, and every container that holds a
// TypeNode (Schema, FieldNode, ArrayNode, MapNode, UnionNode, OneOfNode)
// decodes its child slots through [DecodeType]. An unknown kind is an error
// here — never skipped — per the skeleton-honesty rule.

// kindField is the wire name of the discriminator.
const kindField = "kind"

// nodeAlias types strip the methods so marshaling the alias does not recurse
// back into MarshalJSON.
type (
	objectAlias   ObjectNode
	stringAlias   StringNode
	intAlias      IntNode
	floatAlias    FloatNode
	boolAlias     BoolNode
	bytesAlias    BytesNode
	timeAlias     TimeNode
	durationAlias DurationNode
	arrayAlias    ArrayNode
	mapAlias      MapNode
	unionAlias    UnionNode
	oneOfAlias    OneOfNode
	refAlias      RefNode
	anyAlias      AnyNode
)

// withKind marshals body and injects {"kind": k} as the first key. Doing it
// by re-encoding into an ordered map would lose field order; splicing the
// key into the object's opening brace keeps the emitted document stable and
// human-readable.
func withKind(k Kind, body any) ([]byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// body always marshals to a JSON object; splice after the opening brace.
	if len(raw) < 2 || raw[0] != '{' {
		return nil, fmt.Errorf("ir: node %q did not marshal to an object", k)
	}
	head := fmt.Sprintf(`{"%s":%q`, kindField, string(k))
	if bytes.Equal(raw, []byte("{}")) {
		return []byte(head + "}"), nil
	}
	return append([]byte(head+","), raw[1:]...), nil
}

// MarshalJSON implements json.Marshaler.
func (n *ObjectNode) MarshalJSON() ([]byte, error) { return withKind(KindObject, (*objectAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *StringNode) MarshalJSON() ([]byte, error) { return withKind(KindString, (*stringAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *IntNode) MarshalJSON() ([]byte, error) { return withKind(KindInt, (*intAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *FloatNode) MarshalJSON() ([]byte, error) { return withKind(KindFloat, (*floatAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *BoolNode) MarshalJSON() ([]byte, error) { return withKind(KindBool, (*boolAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *BytesNode) MarshalJSON() ([]byte, error) { return withKind(KindBytes, (*bytesAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *TimeNode) MarshalJSON() ([]byte, error) { return withKind(KindTime, (*timeAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *DurationNode) MarshalJSON() ([]byte, error) {
	return withKind(KindDuration, (*durationAlias)(n))
}

// MarshalJSON implements json.Marshaler.
func (n *ArrayNode) MarshalJSON() ([]byte, error) { return withKind(KindArray, (*arrayAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *MapNode) MarshalJSON() ([]byte, error) { return withKind(KindMap, (*mapAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *UnionNode) MarshalJSON() ([]byte, error) { return withKind(KindUnion, (*unionAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *OneOfNode) MarshalJSON() ([]byte, error) { return withKind(KindOneOf, (*oneOfAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *RefNode) MarshalJSON() ([]byte, error) { return withKind(KindRef, (*refAlias)(n)) }

// MarshalJSON implements json.Marshaler.
func (n *AnyNode) MarshalJSON() ([]byte, error) { return withKind(KindAny, (*anyAlias)(n)) }

// DecodeType decodes one serialized type node, dispatching on its "kind".
// A missing or unknown kind is an error, not a nil node: silently producing
// an [AnyNode] would make an unrecognised constraint disappear.
func DecodeType(raw json.RawMessage) (TypeNode, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("ir: type node is null")
	}
	var probe struct {
		Kind Kind `json:"kind"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("ir: decode type node: %w", err)
	}
	if probe.Kind == "" {
		return nil, fmt.Errorf("ir: type node is missing %q", kindField)
	}
	node, ok := newNode(probe.Kind)
	if !ok {
		return nil, fmt.Errorf("ir: unknown type kind %q", probe.Kind)
	}
	if err := json.Unmarshal(raw, node); err != nil {
		return nil, fmt.Errorf("ir: decode %s node: %w", probe.Kind, err)
	}
	return node, nil
}

// decodeTypes decodes a slice of raw nodes.
func decodeTypes(raws []json.RawMessage) ([]TypeNode, error) {
	out := make([]TypeNode, 0, len(raws))
	for i, raw := range raws {
		n, err := DecodeType(raw)
		if err != nil {
			return nil, fmt.Errorf("index %d: %w", i, err)
		}
		out = append(out, n)
	}
	return out, nil
}

// decodeTypeMap decodes a name → raw node map.
func decodeTypeMap(raws map[string]json.RawMessage) (map[string]TypeNode, error) {
	if raws == nil {
		return nil, nil
	}
	out := make(map[string]TypeNode, len(raws))
	for name, raw := range raws {
		n, err := DecodeType(raw)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", name, err)
		}
		out[name] = n
	}
	return out, nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *Schema) UnmarshalJSON(data []byte) error {
	var raw struct {
		Version string                     `json:"version"`
		Root    json.RawMessage            `json:"root"`
		Defs    map[string]json.RawMessage `json:"defs"`
		Meta    SchemaMeta                 `json:"meta"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	root, err := DecodeType(raw.Root)
	if err != nil {
		return fmt.Errorf("ir: root: %w", err)
	}
	defs, err := decodeTypeMap(raw.Defs)
	if err != nil {
		return fmt.Errorf("ir: defs: %w", err)
	}
	*s = Schema{Version: raw.Version, Root: root, Defs: defs, Meta: raw.Meta}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (f *FieldNode) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name       string          `json:"name"`
		JSONName   string          `json:"json"`
		Type       json.RawMessage `json:"type"`
		Optional   bool            `json:"optional"`
		Transforms []Transform     `json:"transforms"`
		Meta       NodeMeta        `json:"meta"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t, err := DecodeType(raw.Type)
	if err != nil {
		return fmt.Errorf("field %q: %w", raw.Name, err)
	}
	*f = FieldNode{Name: raw.Name, JSONName: raw.JSONName, Type: t, Optional: raw.Optional, Transforms: raw.Transforms, Meta: raw.Meta}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *ArrayNode) UnmarshalJSON(data []byte) error {
	var raw struct {
		Base
		Elem  json.RawMessage `json:"elem"`
		Rules []Rule          `json:"rules"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	elem, err := DecodeType(raw.Elem)
	if err != nil {
		return fmt.Errorf("array elem: %w", err)
	}
	*n = ArrayNode{Base: raw.Base, Elem: elem, Rules: raw.Rules}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *MapNode) UnmarshalJSON(data []byte) error {
	var raw struct {
		Base
		Key   json.RawMessage `json:"key"`
		Val   json.RawMessage `json:"val"`
		Rules []Rule          `json:"rules"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	key, err := DecodeType(raw.Key)
	if err != nil {
		return fmt.Errorf("map key: %w", err)
	}
	val, err := DecodeType(raw.Val)
	if err != nil {
		return fmt.Errorf("map val: %w", err)
	}
	*n = MapNode{Base: raw.Base, Key: key, Val: val, Rules: raw.Rules}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *UnionNode) UnmarshalJSON(data []byte) error {
	var raw struct {
		Base
		Variants []json.RawMessage `json:"variants"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	variants, err := decodeTypes(raw.Variants)
	if err != nil {
		return fmt.Errorf("union variants: %w", err)
	}
	*n = UnionNode{Base: raw.Base, Variants: variants}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *OneOfNode) UnmarshalJSON(data []byte) error {
	var raw struct {
		Base
		Discriminator string                     `json:"discriminator"`
		Cases         map[string]json.RawMessage `json:"cases"`
		Default       json.RawMessage            `json:"default"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	cases, err := decodeTypeMap(raw.Cases)
	if err != nil {
		return fmt.Errorf("oneof cases: %w", err)
	}
	var def TypeNode
	if len(raw.Default) > 0 && !bytes.Equal(bytes.TrimSpace(raw.Default), []byte("null")) {
		def, err = DecodeType(raw.Default)
		if err != nil {
			return fmt.Errorf("oneof default: %w", err)
		}
	}
	*n = OneOfNode{Base: raw.Base, Discriminator: raw.Discriminator, Cases: cases, Default: def}
	return nil
}

// sortedKeys returns m's keys in lexical order so walks over maps are
// deterministic — the emitters must produce byte-stable output.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
