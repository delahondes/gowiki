# Code Blocks

## 1. Basic syntax

````
```
Plain code block (no highlighting)
```
````

## 1. Language-specific highlighting

Specify the language after the opening backticks:

````
```python
def fibonacci(n):
    a, b = 0, 1
    for _ in range(n):
        a, b = b, a + b
    return a
```
````

Supported languages include: python, javascript, typescript, go, java, c, cpp, rust, bash, sql, json, yaml, xml, html, css, and many more.

## 1. Editing code blocks

In visual mode:
- **Tab** inserts indentation inside a code block (does not move to the next element)
- **Shift+Tab** removes one level of indentation
- The language specifier can be changed via the property panel

In raw mode, edit the fenced block directly.

## 1. Code theme

The syntax highlighting theme can be changed in Admin > Configuration > Code theme. Both light and dark themes are available.
