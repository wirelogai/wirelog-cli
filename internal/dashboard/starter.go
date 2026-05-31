package dashboard

// StarterYAML is the default agent-friendly dashboard template.
const StarterYAML = `version: 1
title: Product Growth
order: 10
refresh: 60s
timezone: UTC

variables:
  range:
    label: Range
    type: date_range
    default: 30d
  platform:
    label: Platform
    type: select
    default: all
    options:
      - label: All
        value: all
        fragment: ""
      - label: Web
        value: web
        fragment: '| where _platform = "web"'
      - label: iOS
        value: ios
        fragment: '| where _platform = "ios"'
      - label: Android
        value: android
        fragment: '| where _platform = "android"'

sections:
  - title: Overview
    cards:
      - id: events-total
        title: Events
        kind: metric
        viz: number
        layout: {w: 3, h: 2}
        query: '* {{range.stage}} {{platform.fragment}} | count'

      - id: active-users
        title: Active Users
        kind: metric
        viz: number
        layout: {w: 3, h: 2}
        query: '* {{range.stage}} {{platform.fragment}} | unique user_id'

      - id: events-by-day
        title: Events by Day
        kind: chart
        viz: line
        layout: {w: 6, h: 4}
        query: '* {{range.stage}} {{platform.fragment}} | count by day'

  - title: Acquisition
    cards:
      - id: top-events
        title: Top Events
        kind: table
        viz: table
        layout: {w: 6, h: 4}
        query: '* {{range.stage}} {{platform.fragment}} | count by event_type | top 20'

      - id: platform-breakdown
        title: Platform Breakdown
        kind: chart
        viz: bar
        layout: {w: 6, h: 4}
        query: '* {{range.stage}} | count by _platform | top 10'

  - title: Activation
    cards:
      - id: signup-funnel
        title: Signup Funnel
        kind: funnel
        viz: funnel
        layout: {w: 12, h: 5}
        query: funnel page_view -> signup -> activate {{range.stage}}

      - id: activation-trends
        title: Activation Trends
        kind: chart
        viz: line
        layout: {w: 12, h: 4}
        queries:
          - name: Signups
            query: signup {{range.stage}} {{platform.fragment}} | count by day
          - name: Activations
            query: activate {{range.stage}} {{platform.fragment}} | count by day

  - title: Discovery
    cards:
      - id: schema-inspect
        title: Event Schema
        kind: inspect
        viz: table
        layout: {w: 12, h: 5}
        query: inspect * {{range.stage}}

  - title: Notes
    cards:
      - id: operator-notes
        title: Notes
        kind: markdown
        viz: markdown
        layout: {w: 12, h: 2}
        markdown: |
          Agents should replace placeholder event names after running wl inspect.
`

// SchemaJSON is a compact JSON Schema for dashboard authoring tools and agents.
const SchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "WireLog Dashboard",
  "type": "object",
  "required": ["version", "title", "sections"],
  "properties": {
    "version": {"const": 1},
    "title": {"type": "string", "minLength": 1},
    "order": {"type": "integer", "minimum": 0},
    "refresh": {"type": "string"},
    "timezone": {"type": "string"},
    "variables": {
      "type": "object",
      "additionalProperties": {
        "type": "object",
        "required": ["type"],
        "properties": {
          "label": {"type": "string"},
          "type": {"enum": ["select", "input", "date_range"]},
          "default": {"type": "string"},
          "input": {"enum": ["email"]},
          "placeholder": {"type": "string"},
          "submit": {"type": "boolean"},
          "required": {"type": "boolean"},
          "allow_domain_wildcard": {"type": "boolean"},
          "allow_glob": {"type": "boolean"},
          "query": {"type": "string"},
          "value_column": {"type": "string"},
          "label_column": {"type": "string"},
          "fragment_template": {"type": "string"},
          "fragments": {
            "type": "object",
            "additionalProperties": {
              "type": "object",
              "required": ["exact_field"],
              "properties": {
                "exact_field": {"type": "string"},
                "domain_field": {"type": "string"}
              }
            }
          },
          "options": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["label", "value"],
              "properties": {
                "label": {"type": "string"},
                "value": {"type": "string"},
                "fragment": {"type": "string"}
              }
            }
          }
        }
      }
    },
    "sections": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["title", "cards"],
        "properties": {
          "title": {"type": "string"},
          "cards": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["id", "title", "kind", "viz"],
              "properties": {
                "id": {"type": "string"},
                "title": {"type": "string"},
                "kind": {"enum": ["chart", "metric", "table", "events", "funnel", "retention", "journey", "sessions", "lifecycle", "stickiness", "users", "inspect", "markdown"]},
                "viz": {"enum": ["line", "area", "bar", "pie", "number", "table", "event-stream", "funnel", "retention-grid", "path", "lifecycle", "histogram", "json", "markdown"]},
                "query": {"type": "string"},
                "queries": {
                  "type": "array",
                  "items": {
                    "type": "object",
                    "required": ["name", "query"],
                    "properties": {
                      "name": {"type": "string"},
                      "query": {"type": "string"}
                    }
                  }
                },
                "markdown": {"type": "string"},
                "layout": {
                  "type": "object",
                  "properties": {
                    "w": {"type": "integer", "minimum": 1, "maximum": 12},
                    "h": {"type": "integer", "minimum": 1, "maximum": 24}
                  }
                },
                "options": {
                  "type": "object",
                  "properties": {
                    "x": {"type": "string"},
                    "y": {"type": "string"},
                    "series": {"type": "string"},
                    "calculate": {"enum": ["ratio"]},
                    "numerator_y": {"type": "string"},
                    "denominator_y": {"type": "string"}
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}`
