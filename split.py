import sys
import os

def split_file(filepath, chunk_size):
    with open(filepath, 'rb') as f:
        chunk_idx = 0
        while True:
            chunk = f.read(chunk_size)
            if not chunk:
                break
            with open(f"{filepath}.part{chunk_idx:02d}", 'wb') as out_f:
                out_f.write(chunk)
            chunk_idx += 1

if __name__ == '__main__':
    split_file('recorded_data_dump.zip', 75 * 1024 * 1024)  # 75MB
