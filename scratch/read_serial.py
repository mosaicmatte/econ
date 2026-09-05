import serial

ser = serial.Serial('/dev/cu.usbserial-0001', 115200, timeout=1)
ser.dtr = False
ser.rts = False

while True:
    line = ser.readline()
    if line:
        print(line.decode('utf-8', errors='ignore').strip())
