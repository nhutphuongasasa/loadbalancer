#!/bin/bash

# Lấy đường dẫn tuyệt đối đến thư mục chứa script
# sau đó nhảy ra thư mục cha (gốc của dự án)
BASE_DIR=$(cd "$(dirname "$0")"/.. && pwd)

for i in {1..4}; do
    # Sử dụng đường dẫn tuyệt đối dựa trên gốc dự án
    vegeta attack -targets="$BASE_DIR/test/vegeta/client/client$i.txt" \
           -rate=20 -duration=60s \
           -name="Client$i" > "$BASE_DIR/script/results_client$i.bin" &
done

echo "Đang chạy test với 4 clients... Vui lòng đợi 60s."
wait
echo "Hoàn thành! Kết quả đã lưu vào thư mục script/."