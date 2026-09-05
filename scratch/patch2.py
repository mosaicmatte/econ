import re

with open('edge/esp32/src/main.cpp', 'r') as f:
    content = f.read()

content = content.replace("Serial.readStringUntil('\n');", "Serial.readStringUntil('\\n');")
content = content.replace("Serial.readStringUntil('\n\');", "Serial.readStringUntil('\\n');")
content = content.replace("Serial.readStringUntil('\\n\');", "Serial.readStringUntil('\\n');")
content = content.replace("Serial.readStringUntil('\\n\\n');", "Serial.readStringUntil('\\n');")

content = re.sub(r'Serial\.readStringUntil\(\'\n\'\);', "Serial.readStringUntil('\\n');", content)

content = content.replace('"[wifi] connecting to %s\n"', '"[wifi] connecting to %s\\n"')
content = content.replace('"\n[wifi] connected, ip=%s\n"', '"\\n[wifi] connected, ip=%s\\n"')
content = content.replace('"\n[wifi] failed to connect, falling back to local connection."', '"\\n[wifi] failed to connect, falling back to local connection."')
content = content.replace('"[wifi] received new credentials: %s\n"', '"[wifi] received new credentials: %s\\n"')

with open('edge/esp32/src/main.cpp', 'w') as f:
    f.write(content)
