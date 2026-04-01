---
name: code-review
description: Reviews code for quality, style, and potential issues. Use when asked to review code, check for bugs, or improve code quality.
version: "1.0"
tags: [code-quality, review, best-practices]
tools: [read_file, shell]
triggers: [review, code review, check code, 代码审查, 代码检查]
---

# Code Review

When reviewing code, follow these steps systematically:

## Step 1: Understand the Context
- Read the file(s) to be reviewed using the `read_file` tool
- Identify the programming language and framework
- Understand the purpose of the code

## Step 2: Check for Issues
Review the code for the following categories:

### Correctness
- Logic errors and off-by-one mistakes
- Null/nil pointer dereferences
- Unhandled error cases
- Race conditions in concurrent code

### Security
- Input validation and sanitization
- SQL injection, XSS, or other injection vulnerabilities
- Hardcoded secrets or credentials
- Improper permission handling

### Performance
- Unnecessary allocations or copies
- N+1 query problems
- Missing indexes or inefficient data structures
- Unbounded growth (memory leaks)

### Style & Maintainability
- Naming conventions (clear, consistent)
- Function length (prefer < 30 lines)
- Code duplication
- Missing or misleading comments

## Step 3: Provide Feedback
Structure your review as:
1. **Summary**: One-paragraph overview of the code quality
2. **Issues**: List each issue with severity (Critical / Warning / Info)
3. **Suggestions**: Actionable improvement recommendations
4. **Positive**: Highlight what's done well

## Guidelines
- Be constructive, not dismissive
- Provide specific line references when possible
- Suggest concrete fixes, not just "this is wrong"
- Prioritize critical issues over style nitpicks
