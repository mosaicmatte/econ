#include <iostream>
#include <string>

using namespace std;

void parse(string line) {
    if (line.rfind("[wifi] connect ", 0) == 0) {
      string payload = line.substr(15);
      // trim
      size_t first = payload.find_first_not_of(" \t\r\n");
      if (first != string::npos) payload = payload.substr(first);
      size_t last = payload.find_last_not_of(" \t\r\n");
      if (last != string::npos) payload = payload.substr(0, last + 1);
      
      string current_ssid = "";
      string current_pass = "";

      if (payload.find("{") == 0) {
          cout << "JSON parsing branch\n";
      } else if (payload.find("\"") == 0) {
        int firstEnd = payload.find('"', 1);
        if (firstEnd > 0) {
          current_ssid = payload.substr(1, firstEnd - 1);
          string remainder = payload.substr(firstEnd + 1);
          // trim remainder
          first = remainder.find_first_not_of(" \t\r\n");
          if (first != string::npos) remainder = remainder.substr(first);
          else remainder = "";
          last = remainder.find_last_not_of(" \t\r\n");
          if (last != string::npos) remainder = remainder.substr(0, last + 1);
          
          if (remainder.find("\"") == 0 && remainder.rfind("\"") == remainder.length() - 1 && remainder.length() >= 2) {
            current_pass = remainder.substr(1, remainder.length() - 2);
          } else {
            current_pass = remainder;
          }
        }
      } else {
        int space = payload.find(' ');
        if (space > 0) {
          current_ssid = payload.substr(0, space);
          current_pass = payload.substr(space + 1);
        } else {
          current_ssid = payload;
        }
      }

      cout << "SSID: [" << current_ssid << "]\n";
      cout << "PASS: [" << current_pass << "]\n";
    }
}

int main() {
    parse("[wifi] connect \"My SSID\" \"My Pass\"");
    parse("[wifi] connect \"SSID with \\\" quotes\" \"password\""); // fails
    return 0;
}
