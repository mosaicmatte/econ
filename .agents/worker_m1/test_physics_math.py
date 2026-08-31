import math, datetime

# 1. Solar Geometry
def solar_position(dt_utc, lat_deg, lon_deg):
    day_of_year = dt_utc.timetuple().tm_yday
    utc_hour = dt_utc.hour + dt_utc.minute/60.0 + dt_utc.second/3600.0
    gamma = (2.0 * math.pi / 365.0) * (day_of_year - 1.0 + (utc_hour - 12.0) / 24.0)
    eot = 229.18 * (0.000075 + 0.001868*math.cos(gamma) - 0.032077*math.sin(gamma) - 0.014615*math.cos(2*gamma) - 0.040849*math.sin(2*gamma))
    decl = 0.006918 - 0.399912*math.cos(gamma) + 0.070257*math.sin(gamma) - 0.006758*math.cos(2*gamma) + 0.000907*math.sin(2*gamma) - 0.002697*math.cos(3*gamma) + 0.001480*math.sin(3*gamma)
    time_offset = 4.0 * lon_deg + eot
    solar_time = (utc_hour + time_offset/60.0) % 24.0
    omega = (solar_time - 12.0) * 15.0 * (math.pi / 180.0)
    lat_rad = lat_deg * (math.pi / 180.0)
    cos_zenith = math.sin(lat_rad)*math.sin(decl) + math.cos(lat_rad)*math.cos(decl)*math.cos(omega)
    if cos_zenith <= 0:
        return cos_zenith, 0.0, 0.0
    ecc = 1.0 + 0.033*math.cos(2.0*math.pi*day_of_year/365.0)
    i0 = 1361.0 * ecc
    air_mass = 1.0 / max(0.01, cos_zenith)
    dni = i0 * math.exp(-0.18 * air_mass)
    ghi = dni * cos_zenith * 1.10
    return cos_zenith, dni, ghi

# Verify midnight vs noon
midnight_utc = datetime.datetime(2026, 8, 31, 17, 0, 0) # 00:00 ICT
noon_utc = datetime.datetime(2026, 8, 31, 5, 0, 0) # 12:00 ICT

cz_m, dni_m, ghi_m = solar_position(midnight_utc, 10.8231, 106.6297)
cz_n, dni_n, ghi_n = solar_position(noon_utc, 10.8231, 106.6297)

assert ghi_m == 0.0, f'Midnight GHI must be 0.0, got {ghi_m}'
assert ghi_n > 800.0, f'Noon GHI must be > 800 W/m2, got {ghi_n}'
print(f'Solar Geometry: Midnight GHI={ghi_m:.1f} W/m2, Noon GHI={ghi_n:.1f} W/m2 (cos_zenith={cz_n:.3f})')

# 2. Diurnal Weather Fallback
def outdoor_fallback(hour):
    phase = 2.0 * math.pi * (hour - 15.0) / 24.0
    temp = 29.5 + 4.5 * math.cos(phase)
    hum = 75.0 - 20.0 * math.cos(phase)
    return temp, hum

t_15, h_15 = outdoor_fallback(15)
t_3, h_3 = outdoor_fallback(3)
assert math.isclose(t_15, 34.0, abs_tol=1e-5), f'T(15) want 34.0, got {t_15}'
assert math.isclose(t_3, 25.0, abs_tol=1e-5), f'T(3) want 25.0, got {t_3}'
assert math.isclose(h_15, 55.0, abs_tol=1e-5), f'RH(15) want 55.0, got {h_15}'
assert math.isclose(h_3, 95.0, abs_tol=1e-5), f'RH(3) want 95.0, got {h_3}'
print(f'Diurnal Weather: Peak(15:00)={t_15:.1f}C/{h_15:.0f}%RH, Trough(03:00)={t_3:.1f}C/{h_3:.0f}%RH')

# 3. Thermodynamic COP
def calc_cop(t_out, t_sup, q_w, area_m2, strain):
    t_cond_k = t_out + 5.0 + 273.15
    t_evap_k = t_sup - 3.0 + 273.15
    lift = max(2.0, t_cond_k - t_evap_k)
    cop_carnot = t_evap_k / lift
    design_q = max(10000.0, area_m2 * 120.0)
    plr = max(0.1, min(1.2, q_w / design_q))
    f_plr = 0.15 + 1.25*plr - 0.40*plr*plr
    strain_factor = max(0.70, 1.0 - 0.05*strain)
    cop = 0.35 * cop_carnot * f_plr * strain_factor
    return max(2.2, min(3.8, cop))

cop_25 = calc_cop(25.0, 12.0, 150000.0, 1200.0, 0.0)
cop_38 = calc_cop(38.0, 12.0, 150000.0, 1200.0, 0.0)
assert cop_25 > cop_38, f'COP at 25C ({cop_25:.2f}) must exceed 38C ({cop_38:.2f})'
degradation = (cop_25 - cop_38) / cop_25 * 100
assert degradation >= 20.0, f'Degradation was only {degradation:.1f}%'
print(f'Thermodynamic COP: Mild(25C)={cop_25:.2f}, Extreme(38C)={cop_38:.2f} (Degradation={degradation:.1f}%)')

# 4. Dynamic Supply Air
def calc_supply_air(t_out, t_ret):
    alpha = 0.15
    t_mixed = alpha * t_out + (1 - alpha) * t_ret
    t_chilled = 7.0
    eff = 0.80
    t_sup = t_mixed - eff * (t_mixed - t_chilled)
    return max(8.0, min(18.0, t_sup))

sup_hot = calc_supply_air(38.0, 26.0)
sup_cool = calc_supply_air(22.0, 24.0)
assert sup_hot > sup_cool, f'Hot supply ({sup_hot:.2f}) must be higher than cool ({sup_cool:.2f})'
print(f'Dynamic Supply Air: Extreme Hot Ambient={sup_hot:.2f}C, Cool Ambient={sup_cool:.2f}C')

print('ALL PHYSICS VERIFICATIONS PASSED SUCCESSFULLY!')
