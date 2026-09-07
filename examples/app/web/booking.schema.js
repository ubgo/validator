// Generated from the Go rules; do not edit.

export const bookingSchema = {
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://example.com/schemas/booking.json",
  "title": "Booking",
  "type": "object",
  "properties": {
    "email": {
      "type": "string",
      "format": "email"
    },
    "phone": {
      "type": "string",
      "pattern": "^\\+[1-9]\\d{1,14}$"
    },
    "check_in": {
      "type": "string",
      "format": "date-time"
    },
    "check_out": {
      "type": "string",
      "format": "date-time"
    },
    "guests": {
      "type": "integer",
      "minimum": 1,
      "maximum": 4
    },
    "room": {
      "type": "string",
      "enum": [
        "single",
        "double",
        "suite"
      ]
    },
    "note": {
      "type": "string",
      "minLength": 10,
      "maxLength": 500
    },
    "extras": {
      "type": "array",
      "items": {
        "type": "string",
        "enum": [
          "breakfast",
          "parking",
          "late-checkout"
        ]
      },
      "maxItems": 3,
      "uniqueItems": true
    }
  },
  "required": [
    "email",
    "check_in",
    "check_out",
    "guests",
    "room"
  ],
  "allOf": [
    {
      "if": {
        "allOf": [
          {
            "properties": {
              "room": {
                "const": "suite"
              }
            },
            "required": [
              "room"
            ]
          },
          {
            "properties": {
              "guests": {
                "maximum": 1
              }
            },
            "required": [
              "guests"
            ]
          }
        ]
      },
      "then": {
        "required": [
          "note"
        ]
      }
    }
  ]
};

export const bookingManifest = {
  "entries": [
    {
      "path": "$.email",
      "kind": "rule",
      "name": "async",
      "class": "server",
      "ref": "email_free",
      "endpoint": "/v/email_free",
      "reason": "async rule runs server-side; frontend round-trips"
    },
    {
      "path": "$.email",
      "kind": "transform",
      "name": "trim",
      "class": "lossy",
      "reason": "JSON Schema validates, it does not transform; applied server-side"
    },
    {
      "path": "$.email",
      "kind": "transform",
      "name": "lower",
      "class": "lossy",
      "reason": "JSON Schema validates, it does not transform; applied server-side"
    },
    {
      "path": "$.check_out",
      "kind": "rule",
      "name": "after",
      "class": "cel",
      "ref": "CheckIn",
      "reason": "JSON Schema cannot compare two properties; carried as a condition a runtime can evaluate",
      "cond": {
        "kind": "fieldcmp",
        "field": "check_out",
        "op": "gt",
        "fieldB": "check_in"
      },
      "code": "after",
      "message": "must be after check_in"
    },
    {
      "path": "$",
      "kind": "rule",
      "name": "custom",
      "class": "opaque",
      "ref": "fits_the_room",
      "reason": "custom Go function runs server-side only"
    },
    {
      "path": "$",
      "kind": "rule",
      "name": "custom",
      "class": "opaque",
      "ref": "stay_length",
      "reason": "custom Go function runs server-side only"
    }
  ]
};
