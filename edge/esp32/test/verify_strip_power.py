#!/usr/bin/env python3
"""
Adversarial Verification Harness for ACS712 True-RMS Power Algorithm
ECON Project - Edge Firmware (edge/esp32)
"""

import math
import random
import sys
from typing import List, Optional, Tuple

# Configuration Defaults matching node_config.h
DEFAULT_STRIP_CAL_A_PER_V = 15.0
DEFAULT_PLUG_MAINS_V = 230.0
ADC_MAX_COUNTS = 4095.0
ADC_VREF = 3.3
NOISE_FLOOR_COUNTS = 12.0
MIN_SAMPLE_COUNT = 100


def simulate_esp32_read_strip_amps(
    samples: List[int],
    cal_a_per_v: float = DEFAULT_STRIP_CAL_A_PER_V
) -> float:
    """
    Exact implementation mirror of readStripAmps() in edge/esp32/src/main.cpp:
    
    double sum = 0, sumSq = 0;
    int n = 0;
    for (int v : samples) {
        sum += v;
        sumSq += (double)v * v;
        n++;
    }
    if (n < 100) return -1;
    double mean = sum / n;
    double rmsCounts = sqrt(fmax(0.0, sumSq / n - mean * mean));
    const double STRIP_NOISE_FLOOR_COUNTS = 12.0;
    if (rmsCounts < STRIP_NOISE_FLOOR_COUNTS) return 0.0f;
    return (float)(rmsCounts * (3.3 / 4095.0) * stripCalAPerV);
    """
    if len(samples) < MIN_SAMPLE_COUNT:
        return -1.0

    sum_v = 0.0
    sum_sq = 0.0
    n = 0
    for v in samples:
        sum_v += v
        sum_sq += float(v) * float(v)
        n += 1

    if n < MIN_SAMPLE_COUNT:
        return -1.0

    mean = sum_v / n
    variance = max(0.0, (sum_sq / n) - (mean * mean))
    rms_counts = math.sqrt(variance)
    if rms_counts < NOISE_FLOOR_COUNTS:
        return 0.0
    return rms_counts * (ADC_VREF / ADC_MAX_COUNTS) * cal_a_per_v


def simulate_esp32_publish(
    strip_amps: float,
    mains_v: float = DEFAULT_PLUG_MAINS_V
) -> Optional[float]:
    """
    Exact implementation mirror of readAndPublish() in edge/esp32/src/main.cpp:
    
    if (stripAmps >= 0) {
        doc["stripW"] = round(stripAmps * gCfg.plugMainsV * 10) / 10.0;
    } else {
        Serial.println("[strip] ADC window starved -> omitted");
    }
    """
    if strip_amps >= 0.0:
        return round(strip_amps * mains_v * 10.0) / 10.0
    return None  # Field omitted


def generate_adc_samples(
    duration_s: float = 0.100,
    sample_rate_hz: float = 10000.0,
    dc_offset_v: float = 2.5,
    ac_peak_amps: float = 0.0,
    cal_a_per_v: float = DEFAULT_STRIP_CAL_A_PER_V,
    freq_hz: float = 50.0,
    harmonics: Optional[List[Tuple[int, float]]] = None,  # (harmonic_order, amp_relative_to_fundamental)
    noise_sigma_counts: float = 0.0,
    clip_min_counts: int = 0,
    clip_max_counts: int = 4095,
    starve_samples: Optional[int] = None,
    jitter_fraction: float = 0.0
) -> List[int]:
    """
    Generates synthetic ADC counts sampled over duration_s.
    ACS712 converts current to voltage: V_ac = I / cal_a_per_v.
    V_total = dc_offset_v + V_ac.
    ADC_count = round(V_total * 4095 / 3.3).
    """
    if starve_samples is not None:
        total_samples = starve_samples
    else:
        total_samples = int(duration_s * sample_rate_hz)

    samples = []
    t = 0.0
    dt = 1.0 / sample_rate_hz if sample_rate_hz > 0 else 0.0001

    v_peak = ac_peak_amps / cal_a_per_v if cal_a_per_v > 0 else 0.0

    for i in range(total_samples):
        current_t = t
        if jitter_fraction > 0.0:
            current_t += random.uniform(-dt * jitter_fraction, dt * jitter_fraction)

        # Fundamental
        v_ac = v_peak * math.sin(2.0 * math.pi * freq_hz * current_t)

        # Harmonics
        if harmonics:
            for h_order, h_ratio in harmonics:
                v_ac += (v_peak * h_ratio) * math.sin(2.0 * math.pi * freq_hz * h_order * current_t)

        v_total = dc_offset_v + v_ac

        # Ideal count
        raw_count = v_total * (ADC_MAX_COUNTS / ADC_VREF)

        # Add Gaussian noise
        if noise_sigma_counts > 0.0:
            raw_count += random.gauss(0.0, noise_sigma_counts)

        count_int = int(round(raw_count))
        # Clamp to ADC hardware limits (0..4095)
        count_clamped = max(clip_min_counts, min(clip_max_counts, count_int))
        samples.append(count_clamped)

        t += dt

    return samples


def run_tests():
    print("=================================================================")
    print("ECON ACS712 TRUE-RMS POWER ALGORITHM ADVERSARIAL VERIFICATION")
    print("=================================================================")
    all_passed = True
    random.seed(42)

    # -------------------------------------------------------------
    # Test 1: DC Offset Subtraction Invariance (Zero AC Signal)
    # -------------------------------------------------------------
    print("\n[Test 1] DC Offset Subtraction (Zero Current, Various DC Offsets)")
    dc_offsets = [0.0, 0.5, 1.2, 1.65, 2.0, 2.5, 2.8, 3.3]
    for dc in dc_offsets:
        samples = generate_adc_samples(duration_s=0.100, dc_offset_v=dc, ac_peak_amps=0.0)
        amps = simulate_esp32_read_strip_amps(samples)
        watts = simulate_esp32_publish(amps)
        mean_cnt = sum(samples) / len(samples)
        status = "PASS" if amps == 0.0 and watts == 0.0 else "FAIL"
        if status == "FAIL":
            all_passed = False
        print(f"  DC Offset: {dc:4.2f}V (mean ADC: {mean_cnt:6.1f}) -> amps: {amps:4.2f}A, stripW: {watts}W [{status}]")

    # -------------------------------------------------------------
    # Test 2: DC Offset Subtraction Invariance With Known AC Signal
    # 5A peak = 3.5355A RMS. 230V mains -> theoretical 813.17W -> round 813.2W
    # -------------------------------------------------------------
    print("\n[Test 2] DC Offset Subtraction with 5A Peak AC Signal (3.5355A RMS)")
    # Note: 5A peak with 15 A/V is ~0.333V peak.
    # At 1.65V DC: swing is 1.32V to 1.98V (well within 0..3.3V)
    # At 2.50V DC: swing is 2.17V to 2.83V (well within 0..3.3V)
    # At 2.90V DC: swing is 2.57V to 3.23V (within 0..3.3V)
    valid_dcs = [1.0, 1.65, 2.0, 2.5, 2.8]
    expected_rms = 5.0 / math.sqrt(2.0)  # 3.5355339 A
    expected_w = round(expected_rms * DEFAULT_PLUG_MAINS_V * 10.0) / 10.0  # 813.2 W

    for dc in valid_dcs:
        samples = generate_adc_samples(
            duration_s=0.100,
            sample_rate_hz=10000,
            dc_offset_v=dc,
            ac_peak_amps=5.0,
            freq_hz=50.0
        )
        amps = simulate_esp32_read_strip_amps(samples)
        watts = simulate_esp32_publish(amps)
        err_pct = abs(amps - expected_rms) / expected_rms * 100.0
        # ADC quantization error with 10k samples over 5 cycles is < 0.05%
        # 1 ADC count = 2.78W; quantization discretization is +-0.2W
        status = "PASS" if abs(watts - expected_w) <= 0.3 and err_pct < 0.05 else "FAIL"
        if status == "FAIL":
            all_passed = False
        print(f"  DC: {dc:4.2f}V -> amps: {amps:6.4f}A (err {err_pct:5.3f}%), stripW: {watts:5.1f}W (exp {expected_w}W) [{status}]")

    # -------------------------------------------------------------
    # Test 3: Pure Noise vs. Noise Floor Gate (< 12.0 counts -> 0.0W)
    # -------------------------------------------------------------
    print("\n[Test 3] Pure Noise & Sub-Threshold Signal Gate (< 12.0 counts -> 0.0W)")
    # Typical ESP32 ADC noise standard deviation is 6 to 10 counts RMS
    noise_sigmas = [0.5, 1.0, 2.0, 4.0, 6.0, 8.0, 8.5, 10.0, 11.0]
    for sigma in noise_sigmas:
        samples = generate_adc_samples(
            duration_s=0.100,
            dc_offset_v=2.5,
            ac_peak_amps=0.0,
            noise_sigma_counts=sigma
        )
        amps = simulate_esp32_read_strip_amps(samples)
        watts = simulate_esp32_publish(amps)
        raw_amps_equiv = sigma * (ADC_VREF / ADC_MAX_COUNTS) * DEFAULT_STRIP_CAL_A_PER_V
        expected_gated = sigma < NOISE_FLOOR_COUNTS
        is_zero = (amps == 0.0 and watts == 0.0)
        status = "PASS" if is_zero == expected_gated else "FAIL"
        if status == "FAIL":
            all_passed = False
        print(f"  Noise sigma: {sigma:4.1f} cnt (equiv ~{raw_amps_equiv:5.3f}A) -> amps: {amps:5.3f}A, stripW: {watts:4.1f}W [{status}]")

    # Test small sine signal right below and above noise floor
    # Sub-threshold: 0.08A RMS (0.113A peak, ~6.6 counts < 12.0) -> should be 0.0W
    samples_sub = generate_adc_samples(
        duration_s=0.100, dc_offset_v=2.5, ac_peak_amps=0.08 * math.sqrt(2)
    )
    amps_sub = simulate_esp32_read_strip_amps(samples_sub)
    w_sub = simulate_esp32_publish(amps_sub)
    status_sub = "PASS" if amps_sub == 0.0 and w_sub == 0.0 else "FAIL"
    print(f"  Sine 0.08A RMS (< noise floor) -> amps: {amps_sub:4.2f}A, stripW: {w_sub}W [{status_sub}]")
    if status_sub == "FAIL":
        all_passed = False

    # Above-threshold: 0.15A RMS (0.212A peak, ~12.4 counts > 12.0) -> should be 0.15A, ~34.5W
    samples_above = generate_adc_samples(
        duration_s=0.100, dc_offset_v=2.5, ac_peak_amps=0.15 * math.sqrt(2)
    )
    amps_above = simulate_esp32_read_strip_amps(samples_above)
    w_above = simulate_esp32_publish(amps_above)
    status_above = "PASS" if abs(amps_above - 0.15) < 0.005 and abs(w_above - 34.5) < 0.5 else "FAIL"
    print(f"  Sine 0.15A RMS (> noise floor) -> amps: {amps_above:5.3f}A, stripW: {w_above}W [{status_above}]")
    if status_above == "FAIL":
        all_passed = False

    # -------------------------------------------------------------
    # Test 4: Known AC Currents Accuracy Benchmark (at 230V mains)
    # -------------------------------------------------------------
    print("\n[Test 4] Known AC Current Accuracy Benchmark (1A, 3.535A, 5A, 10A, 16A)")
    test_currents_rms = [0.5, 1.0, 2.0, 3.5355339, 5.0, 8.0, 10.0, 16.0]
    for i_rms in test_currents_rms:
        i_peak = i_rms * math.sqrt(2.0)
        # Use 1.65V DC center for wide dynamic range up to 16A peak (16A / 15 A/V = 1.06V peak -> swing 0.59V..2.71V)
        samples = generate_adc_samples(
            duration_s=0.100,
            sample_rate_hz=10000,
            dc_offset_v=1.65,
            ac_peak_amps=i_peak,
            freq_hz=50.0
        )
        calc_amps = simulate_esp32_read_strip_amps(samples)
        calc_w = simulate_esp32_publish(calc_amps)
        exp_w = round(i_rms * DEFAULT_PLUG_MAINS_V * 10.0) / 10.0
        abs_err_w = abs(calc_w - exp_w)
        rel_err = abs_err_w / exp_w * 100.0
        # 1 ADC count = 2.78W. For small signals (0.5A = 115W), +-0.2W discretization is 0.17%.
        # Pass if either abs error <= 0.3W or relative error < 0.1%
        status = "PASS" if abs_err_w <= 0.3 or rel_err < 0.1 else "FAIL"
        if status == "FAIL":
            all_passed = False
        print(f"  Target: {i_rms:7.4f}A RMS -> calc amps: {calc_amps:7.4f}A, stripW: {calc_w:6.1f}W (exp: {exp_w:6.1f}W, err: {rel_err:5.3f}%) [{status}]")

    # -------------------------------------------------------------
    # Test 5: Starved Sampling Window (Sample count < 100 -> Omission)
    # -------------------------------------------------------------
    print("\n[Test 5] Starved Sampling Guard (< 100 samples -> -1 -> stripW omitted)")
    sample_counts = [0, 1, 10, 50, 98, 99, 100, 101, 500]
    for count in sample_counts:
        samples = generate_adc_samples(
            dc_offset_v=2.5,
            ac_peak_amps=5.0,
            starve_samples=count
        )
        amps = simulate_esp32_read_strip_amps(samples)
        pub = simulate_esp32_publish(amps)
        if count < 100:
            expected_omission = (amps == -1.0 and pub is None)
            status = "PASS" if expected_omission else "FAIL"
            print(f"  Sample count N={count:3d} (< 100) -> amps: {amps:4.1f}, stripW: {pub} (OMITTED) [{status}]")
        else:
            expected_valid = (amps >= 0.0 and pub is not None)
            status = "PASS" if expected_valid else "FAIL"
            print(f"  Sample count N={count:3d} (>=100) -> amps: {amps:6.4f}A, stripW: {pub}W (PUBLISHED) [{status}]")
        if status == "FAIL":
            all_passed = False

    # -------------------------------------------------------------
    # Test 6: Adversarial Challenge - Signal Clipping & Saturation
    # -------------------------------------------------------------
    print("\n[Test 6] Adversarial Challenge: ADC Saturation / Clipping at 2.5V DC Offset")
    # At 2.5V DC without voltage divider, clipping occurs at 3.3V (4095 counts), headroom is only 0.8V.
    # 0.8V * 15 A/V = 12.0A peak (8.48A RMS).
    # When current exceeds 12A peak, positive half clips at 4095 while negative half reaches 1.7V.
    currents_for_clipping = [5.0, 10.0, 12.0, 15.0, 20.0]
    for i_peak in currents_for_clipping:
        i_rms_true = i_peak / math.sqrt(2.0)
        samples = generate_adc_samples(
            duration_s=0.100,
            dc_offset_v=2.5,
            ac_peak_amps=i_peak,
            clip_max_counts=4095,
            clip_min_counts=0
        )
        clipped_samples_count = sum(1 for s in samples if s == 4095)
        calc_amps = simulate_esp32_read_strip_amps(samples)
        calc_w = simulate_esp32_publish(calc_amps)
        true_w = round(i_rms_true * DEFAULT_PLUG_MAINS_V * 10.0) / 10.0
        clip_pct = (clipped_samples_count / len(samples)) * 100.0
        err_pct = (calc_w - true_w) / true_w * 100.0
        note = "CLIPPED!" if clipped_samples_count > 0 else "clean"
        print(f"  Peak: {i_peak:4.1f}A ({i_rms_true:5.2f}A RMS) -> clipped: {clip_pct:4.1f}%, stripW: {calc_w:6.1f}W (true: {true_w:6.1f}W, err: {err_pct:5.1f}%) [{note}]")

    # -------------------------------------------------------------
    # Test 7: Adversarial Challenge - Non-Linear Loads & Harmonics
    # -------------------------------------------------------------
    print("\n[Test 7] Adversarial Challenge: Non-Linear Loads (3rd, 5th, 7th Harmonics)")
    # SMPS (laptop power supply): Fundamental 2.0A peak, 3rd harm 60% (1.2A), 5th harm 30% (0.6A)
    # True RMS of harmonic sum: sqrt(sum(I_k_rms^2))
    # Fundamental peak: 2.0A -> RMS: 1.414A
    # 3rd harm peak: 1.2A -> RMS: 0.849A
    # 5th harm peak: 0.6A -> RMS: 0.424A
    # True total RMS = sqrt(1.4142^2 + 0.8485^2 + 0.4243^2) = sqrt(2.0 + 0.72 + 0.18) = sqrt(2.9) = 1.7029 A
    harm_profile = [(3, 0.60), (5, 0.30)]
    samples_harm = generate_adc_samples(
        duration_s=0.100,
        dc_offset_v=1.65,
        ac_peak_amps=2.0,
        harmonics=harm_profile
    )
    calc_amps_harm = simulate_esp32_read_strip_amps(samples_harm)
    calc_w_harm = simulate_esp32_publish(calc_amps_harm)
    true_amps_harm = math.sqrt(2.0**2 / 2.0 + (2.0 * 0.6)**2 / 2.0 + (2.0 * 0.3)**2 / 2.0)
    true_w_harm = round(true_amps_harm * DEFAULT_PLUG_MAINS_V * 10.0) / 10.0
    err_harm_pct = abs(calc_amps_harm - true_amps_harm) / true_amps_harm * 100.0
    status_harm = "PASS" if err_harm_pct < 0.2 else "FAIL"
    if status_harm == "FAIL":
        all_passed = False
    print(f"  Harmonic SMPS waveform: true RMS: {true_amps_harm:6.4f}A -> calc: {calc_amps_harm:6.4f}A (err {err_harm_pct:5.3f}%), stripW: {calc_w_harm:5.1f}W (exp {true_w_harm:5.1f}W) [{status_harm}]")

    # -------------------------------------------------------------
    # Test 8: Adversarial Challenge - Mains Frequency Deviation (49.0Hz .. 51.0Hz)
    # -------------------------------------------------------------
    print("\n[Test 8] Adversarial Challenge: Mains Frequency Deviation over 100ms Window")
    freqs = [48.0, 49.0, 49.5, 50.0, 50.5, 51.0, 52.0, 60.0]
    expected_5a_rms = 5.0 / math.sqrt(2.0)  # 3.5355A
    for f in freqs:
        samples_f = generate_adc_samples(
            duration_s=0.100,
            dc_offset_v=1.65,
            ac_peak_amps=5.0,
            freq_hz=f
        )
        calc_amps_f = simulate_esp32_read_strip_amps(samples_f)
        calc_w_f = simulate_esp32_publish(calc_amps_f)
        err_f_pct = abs(calc_amps_f - expected_5a_rms) / expected_5a_rms * 100.0
        # Over 100ms: at 50Hz = 5.0 cycles (0% leakage), at 60Hz = 6.0 cycles (0% leakage).
        # At 49Hz = 4.9 cycles -> max leakage error ~0.5%
        print(f"  Grid freq: {f:4.1f} Hz -> calc amps: {calc_amps_f:6.4f}A (leakage err: {err_f_pct:5.3f}%), stripW: {calc_w_f:5.1f}W")

    # -------------------------------------------------------------
    # Test 9: Calibration Range Boundary Testing (stripCalAPerV 1.0 .. 500.0)
    # -------------------------------------------------------------
    print("\n[Test 9] Calibration Multiplier Range Verification (1.0 .. 500.0 A/V)")
    cal_values = [1.0, 15.0, 30.0, 66.6, 500.0]
    for cal in cal_values:
        # 1V RMS at ADC pin
        v_peak = math.sqrt(2.0)
        # raw adc swing
        samples_cal = []
        n_pts = 1000
        for k in range(n_pts):
            v = 1.65 + 1.0 * math.sqrt(2.0) * math.sin(2.0 * math.pi * 50.0 * (k / 10000.0))
            samples_cal.append(int(round(v * 4095.0 / 3.3)))
        calc_amps_cal = simulate_esp32_read_strip_amps(samples_cal, cal_a_per_v=cal)
        exp_amps_cal = 1.0 * cal
        err_cal = abs(calc_amps_cal - exp_amps_cal) / exp_amps_cal * 100.0
        status_cal = "PASS" if err_cal < 0.1 else "FAIL"
        if status_cal == "FAIL":
            all_passed = False
        print(f"  Cal: {cal:5.1f} A/V -> 1V RMS input -> {calc_amps_cal:6.2f} A (exp: {exp_amps_cal:6.2f} A, err: {err_cal:5.3f}%) [{status_cal}]")

    # -------------------------------------------------------------
    # Test 10: R3 Waveform Reconstruction with ESP32 ADC Noise (0A, 0.5A, 2A, 10A)
    # -------------------------------------------------------------
    print("\n[Test 10] R3 Waveform Reconstruction with ESP32 ADC Noise (0A, 0.5A, 2A, 10A)")
    # Test 0A with typical ESP32 ADC noise (sigma = 8.0 counts): must be 0.0A (prevent ghost readings)
    samples_0a = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=0.0, noise_sigma_counts=8.0)
    amps_0a = simulate_esp32_read_strip_amps(samples_0a)
    status_0a = "PASS" if amps_0a == 0.0 else "FAIL"
    if status_0a == "FAIL":
        all_passed = False
    print(f"  0A load with typical noise (sigma=8.0 counts) -> amps: {amps_0a:.4f}A [{status_0a}]")

    # Test 0A with elevated ESP32 ADC noise (sigma = 10.0 counts): must be 0.0A
    samples_0a_high = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=0.0, noise_sigma_counts=10.0)
    amps_0a_high = simulate_esp32_read_strip_amps(samples_0a_high)
    status_0a_high = "PASS" if amps_0a_high == 0.0 else "FAIL"
    if status_0a_high == "FAIL":
        all_passed = False
    print(f"  0A load with elevated noise (sigma=10.0 counts) -> amps: {amps_0a_high:.4f}A [{status_0a_high}]")

    # Test known AC loads with typical noise (sigma = 8.0 counts): must reconstruct within 5% accuracy
    test_currents = [0.5, 2.0, 10.0]
    for target in test_currents:
        samples_target = generate_adc_samples(
            duration_s=0.100,
            dc_offset_v=1.65,
            ac_peak_amps=target * math.sqrt(2.0),
            noise_sigma_counts=8.0,
            freq_hz=50.0
        )
        calc_target = simulate_esp32_read_strip_amps(samples_target)
        err_target_pct = abs(calc_target - target) / target * 100.0
        status_target = "PASS" if err_target_pct <= 5.0 else "FAIL"
        if status_target == "FAIL":
            all_passed = False
        print(f"  Target {target:5.1f}A RMS with noise (sigma=8.0) -> calc: {calc_target:.4f}A (err {err_target_pct:.2f}%) [{status_target}]")

    # -------------------------------------------------------------
    # Test 11: Multi-Model ACS712 Sensitivity Scaling (5A, 20A, 30A)
    # -------------------------------------------------------------
    print("\n[Test 11] Multi-Model ACS712 Sensitivity Scaling (5A, 20A, 30A)")
    # ACS712-05B (5.4 A/V): 12.0 counts = 0.052A RMS (~12.0W at 230V)
    # 0.08A RMS (~18.4W) is detected (> 12.0 counts) whereas fixed 0.10A threshold would suppress it
    cal_05b = 5.4
    samples_05b_0a = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=0.0, cal_a_per_v=cal_05b, noise_sigma_counts=8.0)
    amps_05b_0a = simulate_esp32_read_strip_amps(samples_05b_0a, cal_a_per_v=cal_05b)
    status_05b_0a = "PASS" if amps_05b_0a == 0.0 else "FAIL"
    if status_05b_0a == "FAIL":
        all_passed = False
    print(f"  ACS712-05B (5.4 A/V): 0A with noise -> amps: {amps_05b_0a:.4f}A [{status_05b_0a}]")

    samples_05b_light = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=0.08 * math.sqrt(2.0), cal_a_per_v=cal_05b, noise_sigma_counts=8.0)
    amps_05b_light = simulate_esp32_read_strip_amps(samples_05b_light, cal_a_per_v=cal_05b)
    err_05b_light = abs(amps_05b_light - 0.08) / 0.08 * 100.0
    status_05b_light = "PASS" if amps_05b_light > 0.0 and err_05b_light <= 10.0 else "FAIL"
    if status_05b_light == "FAIL":
        all_passed = False
    print(f"  ACS712-05B (5.4 A/V): light 0.08A load -> amps: {amps_05b_light:.4f}A (err {err_05b_light:.2f}%) [{status_05b_light}]")

    samples_05b_15 = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=0.15 * math.sqrt(2.0), cal_a_per_v=cal_05b, noise_sigma_counts=8.0)
    amps_05b_15 = simulate_esp32_read_strip_amps(samples_05b_15, cal_a_per_v=cal_05b)
    err_05b_15 = abs(amps_05b_15 - 0.15) / 0.15 * 100.0
    status_05b_15 = "PASS" if err_05b_15 <= 5.0 else "FAIL"
    if status_05b_15 == "FAIL":
        all_passed = False
    print(f"  ACS712-05B (5.4 A/V): 0.15A load with noise -> amps: {amps_05b_15:.4f}A (err {err_05b_15:.2f}%) [{status_05b_15}]")

    # ACS712-20A (10.0 A/V): 12.0 counts = 0.097A RMS (~22.2W at 230V)
    cal_20a = 10.0
    samples_20a_0a = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=0.0, cal_a_per_v=cal_20a, noise_sigma_counts=8.0)
    amps_20a_0a = simulate_esp32_read_strip_amps(samples_20a_0a, cal_a_per_v=cal_20a)
    status_20a_0a = "PASS" if amps_20a_0a == 0.0 else "FAIL"
    if status_20a_0a == "FAIL":
        all_passed = False
    print(f"  ACS712-20A (10.0 A/V): 0A with noise -> amps: {amps_20a_0a:.4f}A [{status_20a_0a}]")

    samples_20a_load = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=1.0 * math.sqrt(2.0), cal_a_per_v=cal_20a, noise_sigma_counts=8.0)
    amps_20a_load = simulate_esp32_read_strip_amps(samples_20a_load, cal_a_per_v=cal_20a)
    err_20a_load = abs(amps_20a_load - 1.0) / 1.0 * 100.0
    status_20a_load = "PASS" if err_20a_load <= 5.0 else "FAIL"
    if status_20a_load == "FAIL":
        all_passed = False
    print(f"  ACS712-20A (10.0 A/V): 1.0A load with noise -> amps: {amps_20a_load:.4f}A (err {err_20a_load:.2f}%) [{status_20a_load}]")

    # ACS712-30A (15.0 A/V): 12.0 counts = 0.145A RMS (~33.4W at 230V)
    cal_30a = 15.0
    samples_30a_0a = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=0.0, cal_a_per_v=cal_30a, noise_sigma_counts=10.0)
    amps_30a_0a = simulate_esp32_read_strip_amps(samples_30a_0a, cal_a_per_v=cal_30a)
    status_30a_0a = "PASS" if amps_30a_0a == 0.0 else "FAIL"
    if status_30a_0a == "FAIL":
        all_passed = False
    print(f"  ACS712-30A (15.0 A/V): 0A with elevated noise (sigma=10.0) -> amps: {amps_30a_0a:.4f}A [{status_30a_0a}]")

    # -------------------------------------------------------------
    # Test 12: Extreme Edge Cases: Wide Frequency Drift & Motor Inrush
    # -------------------------------------------------------------
    print("\n[Test 12] Extreme Edge Cases: Wide Frequency Drift & Motor Inrush")
    # Extreme frequency wander on cheap portable generators: 45 Hz and 65 Hz over 100ms window
    for f in [45.0, 65.0]:
        samples_f = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=5.0, freq_hz=f)
        amps_f = simulate_esp32_read_strip_amps(samples_f)
        watts_f = simulate_esp32_publish(amps_f)
        err_f_pct = abs(watts_f - 813.2) / 813.2 * 100.0
        status_f = "PASS" if err_f_pct <= 2.0 else "FAIL"
        if status_f == "FAIL":
            all_passed = False
        print(f"  Generator freq {f:4.1f} Hz -> amps: {amps_f:.4f}A, watts: {watts_f:.1f}W (err {err_f_pct:.2f}%) [{status_f}]")

    # Heavy motor inrush current: 30A RMS (42.4A peak) hard-clipping at ADC rails (0V / 3.3V)
    samples_inrush = generate_adc_samples(duration_s=0.100, dc_offset_v=1.65, ac_peak_amps=30.0 * math.sqrt(2.0))
    amps_inrush = simulate_esp32_read_strip_amps(samples_inrush)
    status_inrush = "PASS" if amps_inrush > 0.0 and not math.isnan(amps_inrush) and not math.isinf(amps_inrush) else "FAIL"
    if status_inrush == "FAIL":
        all_passed = False
    print(f"  30A RMS motor inrush hard clipping -> amps: {amps_inrush:.4f}A, watts: {simulate_esp32_publish(amps_inrush):.1f}W [{status_inrush}]")

    print("\n-----------------------------------------------------------------")
    if all_passed:
        print("VERDICT: ALL TESTS PASSED (APPROVE)")
    else:
        print("VERDICT: TEST FAILURES DETECTED (CHALLENGE_FAILED)")
    print("-----------------------------------------------------------------")
    return all_passed


if __name__ == "__main__":
    success = run_tests()
    sys.exit(0 if success else 1)
