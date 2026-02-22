# Agent Skills

Skills are specialized instruction sets and scripts that provide the AI agent with complex capabilities.

## Structure
Each skill should have its own dedicated directory containing a `SKILL.md` file, and optionally other resources:

```text
skills/
└── my-custom-skill/
    ├── SKILL.md
    ├── scripts/
    └── examples/
```

## SKILL.md Format
The `SKILL.md` file must include YAML frontmatter for metadata:

```yaml
---
name: [Name of the skill]
description: [What the skill does and when to use it]
---
```

Follow this with complete instructions on how the agent should utilize the skill.
