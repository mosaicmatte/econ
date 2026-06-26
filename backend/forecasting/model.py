import torch
import torch.nn as nn


class PeakLoadLSTM(nn.Module):
    """Sequence -> single-value regressor for building peak cooling load (MW).

    Input  : (batch, seq_len, input_size) where each timestep is
             [room_temp, airflow, outdoor_temp, outdoor_humidity] (after scaling).
    Output : (batch, 1) predicted peak load.
    """

    def __init__(self, input_size, hidden_size, num_layers, output_size):
        super().__init__()
        self.lstm = nn.LSTM(input_size, hidden_size, num_layers, batch_first=True)
        self.fc = nn.Linear(hidden_size, output_size)

    def forward(self, x):
        # nn.LSTM defaults the hidden/cell state to zeros, so no manual h0/c0 needed.
        out, _ = self.lstm(x)
        return self.fc(out[:, -1, :])  # use only the last timestep's representation
