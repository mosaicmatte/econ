import os

files = [
    'server/simulation/solar.go',
    'server/simulation/engine.go',
    'server/weather.go',
    'server/simulation/hardware_test.go',
    'server/simulation/measured_test.go',
    'server/simulation/sensor_fallback_test.go',
    'server/simulation/sensor_fallback_integration_test.go',
]

for f in files:
    path = os.path.join('/Users/nguyenhoangkhoi/Documents/econ', f)
    code = open(path).read()
    clean = []
    in_str = None
    in_comment = False
    in_block_comment = False
    i = 0
    while i < len(code):
        if not in_str and not in_comment and not in_block_comment:
            if code[i:i+2] == '//':
                in_comment = True
                i += 2
                continue
            elif code[i:i+2] == '/*':
                in_block_comment = True
                i += 2
                continue
            elif code[i] in ('"', "'", '`'):
                in_str = code[i]
                i += 1
                continue
            clean.append(code[i])
        elif in_comment:
            if code[i] == '\n':
                in_comment = False
                clean.append('\n')
        elif in_block_comment:
            if code[i:i+2] == '*/':
                in_block_comment = False
                i += 2
                continue
        elif in_str:
            if in_str != '`' and code[i:i+2] == '\\\\':
                i += 2
                continue
            elif in_str != '`' and code[i:i+2] == '\\' + in_str:
                i += 2
                continue
            elif code[i] == in_str:
                in_str = None
        i += 1
    clean_str = ''.join(clean)
    ob, cb = clean_str.count('{'), clean_str.count('}')
    op, cp = clean_str.count('('), clean_str.count(')')
    osq, csq = clean_str.count('['), clean_str.count(']')
    assert ob == cb, f'{f} braces: {ob} vs {cb}'
    assert op == cp, f'{f} parens: {op} vs {cp}'
    assert osq == csq, f'{f} brackets: {osq} vs {csq}'
    print(f'{f}: Perfectly Balanced! (braces={ob}, parens={op}, brackets={osq})')
