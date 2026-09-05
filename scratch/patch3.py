import re
with open('edge/esp32/src/main.cpp', 'r') as f:
    c = f.read()
c = c.replace('''  }
}
  Serial.printf("\\n[wifi] connected, ip=%s\\n", WiFi.localIP().toString().c_str());
}''', '''  }
}''')
with open('edge/esp32/src/main.cpp', 'w') as f:
    f.write(c)
