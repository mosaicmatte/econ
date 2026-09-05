#!/bin/bash

# generate_hw_system_logs.sh
# Generates a comprehensive report of macOS hardware and system information.
# This script is useful for providing context for debugging and building when disconnected from the hardware.

LOG_DIR="system_logs"
mkdir -p "$LOG_DIR"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LOGFILE="$LOG_DIR/comprehensive_system_hardware_report_$TIMESTAMP.txt"

echo "Gathering comprehensive system and hardware logs..."
echo "This may take a minute or two."

echo "========================================" > "$LOGFILE"
echo " SYSTEM AND HARDWARE REPORT" >> "$LOGFILE"
echo " Date: $(date)" >> "$LOGFILE"
echo "========================================" >> "$LOGFILE"

echo -e "\n\n--- 1. BASIC OS AND KERNEL INFO ---" >> "$LOGFILE"
uname -a >> "$LOGFILE"
sw_vers >> "$LOGFILE"

echo -e "\n\n--- 2. HARDWARE OVERVIEW ---" >> "$LOGFILE"
system_profiler SPHardwareDataType >> "$LOGFILE"

echo -e "\n\n--- 3. CPU DETAILED INFO ---" >> "$LOGFILE"
sysctl hw.model hw.machine hw.ncpu hw.physicalcpu hw.logicalcpu hw.memsize hw.cpufrequency hw.l1icachesize hw.l1dcachesize hw.l2cachesize hw.l3cachesize >> "$LOGFILE" 2>/dev/null
sysctl machdep.cpu >> "$LOGFILE" 2>/dev/null

echo -e "\n\n--- 4. SOFTWARE OVERVIEW ---" >> "$LOGFILE"
system_profiler SPSoftwareDataType >> "$LOGFILE"

echo -e "\n\n--- 5. MEMORY ---" >> "$LOGFILE"
system_profiler SPMemoryDataType >> "$LOGFILE"

echo -e "\n\n--- 6. STORAGE AND DISKS ---" >> "$LOGFILE"
echo "--> system_profiler SPStorageDataType" >> "$LOGFILE"
system_profiler SPStorageDataType >> "$LOGFILE"
echo -e "\n--> diskutil list" >> "$LOGFILE"
diskutil list >> "$LOGFILE"
echo -e "\n--> df -h" >> "$LOGFILE"
df -h >> "$LOGFILE"

echo -e "\n\n--- 7. NETWORK INFO ---" >> "$LOGFILE"
echo "--> system_profiler SPNetworkDataType" >> "$LOGFILE"
system_profiler SPNetworkDataType >> "$LOGFILE"
echo -e "\n--> ifconfig" >> "$LOGFILE"
ifconfig >> "$LOGFILE"

echo -e "\n\n--- 8. DISPLAYS ---" >> "$LOGFILE"
system_profiler SPDisplaysDataType >> "$LOGFILE"

echo -e "\n\n--- 9. POWER & BATTERY ---" >> "$LOGFILE"
system_profiler SPPowerDataType >> "$LOGFILE"

echo -e "\n\n--- 10. USB & THUNDERBOLT DEVICES ---" >> "$LOGFILE"
system_profiler SPUSBDataType >> "$LOGFILE"
system_profiler SPThunderboltDataType >> "$LOGFILE"

echo -e "\n\n--- 11. PCI & BLUETOOTH DEVICES ---" >> "$LOGFILE"
system_profiler SPPCIDataType >> "$LOGFILE"
system_profiler SPBluetoothDataType >> "$LOGFILE"

echo -e "\n\n--- 12. ACTIVE PROCESSES SNAPSHOT ---" >> "$LOGFILE"
top -l 1 -n 20 -s 0 >> "$LOGFILE" 2>/dev/null || echo "top command requires higher privileges, skipping." >> "$LOGFILE"

echo "Done! Report generated at: $(pwd)/$LOGFILE"
