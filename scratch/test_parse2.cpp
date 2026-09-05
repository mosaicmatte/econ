#include <iostream>
#include <string>

using namespace std;

class MockString {
public:
    string s;
    MockString(const char* str) : s(str) {}
    MockString(string str) : s(str) {}
    MockString() : s("") {}
    MockString& operator+=(char c) { s += c; return *this; }
    int length() const { return s.length(); }
    char charAt(int i) const { return s[i]; }
    const char* c_str() const { return s.c_str(); }
};

void parse(string line_str) {
    if (line_str.rfind("[wifi] connect ", 0) == 0) {
      MockString payload = line_str.substr(15);
      
      MockString current_ssid = "";
      MockString current_pass = "";

      int pos = 0;
      auto parseNextToken = [&]() -> MockString {
        while (pos < payload.length() && (payload.charAt(pos) == ' ' || payload.charAt(pos) == '\t')) pos++;
        if (pos >= payload.length()) return "";
        
        MockString token = "";
        bool inQuotes = (payload.charAt(pos) == '"');
        if (inQuotes) pos++;
        
        while (pos < payload.length()) {
          char c = payload.charAt(pos);
          if (inQuotes) {
            if (c == '\\' && pos + 1 < payload.length()) {
              pos++;
              token += payload.charAt(pos);
            } else if (c == '"') {
              pos++;
              break;
            } else {
              token += c;
            }
          } else {
            if (c == ' ' || c == '\t') {
              break;
            } else {
              token += c;
            }
          }
          pos++;
        }
        return token;
      };

      current_ssid = parseNextToken();
      current_pass = parseNextToken();

      cout << "SSID: [" << current_ssid.c_str() << "]\n";
      cout << "PASS: [" << current_pass.c_str() << "]\n";
    }
}

int main() {
    parse("[wifi] connect \"My SSID\" \"My Pass\"");
    parse("[wifi] connect \"SSID with \\\" quotes\" \"password\"");
    parse("[wifi] connect NoQuotesPass NoQuotesPass2");
    parse("[wifi] connect \"SingleQuoted\"");
    parse("[wifi] connect    \"SpacesBefore\"    \"Spaces Between\"   ");
    parse("[wifi] connect \"Malformed escape \\");
    return 0;
}
