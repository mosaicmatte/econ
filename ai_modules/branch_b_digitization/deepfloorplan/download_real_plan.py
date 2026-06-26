"""Download REAL floorplan rasters from Wikimedia Commons, by exact title.

The previous version grabbed the *first* file in `Category:Floor_plans`, which is arbitrary and
once landed on a stained-glass *window* engraving (not a floorplan at all) — poisoning every
Branch B test. Pull known-good plans by explicit title instead. Writes to `samples/`.
"""
import json
import os
import ssl
import urllib.parse
import urllib.request

ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE

# Curated, verified-to-be-actual-floorplans (name -> Commons File: title).
PLANS = {
    "office": "File:Affordable Palace Opposite Office Floorplan1-500.jpg",  # dense plan, courtyard core
    "house":  "File:Benin House Plan.jpg",                                  # clean schematic
}


def _fetch(title, out):
    q = (f"https://commons.wikimedia.org/w/api.php?action=query&format=json"
         f"&titles={urllib.parse.quote(title)}&prop=imageinfo&iiprop=url|size")
    req = urllib.request.Request(q, headers={"User-Agent": "Mozilla/5.0"})
    info = json.loads(urllib.request.urlopen(req, context=ctx, timeout=30).read())
    page = list(info["query"]["pages"].values())[0]["imageinfo"][0]
    url = page["url"]
    req2 = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    data = urllib.request.urlopen(req2, context=ctx, timeout=60).read()
    with open(out, "wb") as f:
        f.write(data)
    print(f"  {out}: {page.get('width')}x{page.get('height')} ({len(data)//1024} KB)")


def main():
    os.makedirs("samples", exist_ok=True)
    for name, title in PLANS.items():
        ext = os.path.splitext(title)[1].lower()
        try:
            _fetch(title, f"samples/{name}{ext}")
        except Exception as e:
            print(f"  {name}: FAILED {e}")


if __name__ == "__main__":
    main()
