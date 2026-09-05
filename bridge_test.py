import threading
import glob
import time

def find_ports():
    ports = glob.glob('/dev/cu.usbserial*') + glob.glob('/dev/cu.usbmodem*')
    return ports

print(find_ports())
