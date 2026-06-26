import re

with open('README.md', 'r') as f:
    text = f.read()

def repl(match):
    inner = match.group(1)
    # Don't touch if it's display math or empty
    if not inner.strip():
        return match.group(0)
    inner = inner.replace('\\_', '_') # Unescape first
    inner = inner.replace('_', '\\_')
    return f'${inner}$'

# Match single $...$ (not $$...$$)
text = re.sub(r'(?<!\$)\$([^$\n]+?)\$(?!\$)', repl, text)

with open('README.md', 'w') as f:
    f.write(text)
