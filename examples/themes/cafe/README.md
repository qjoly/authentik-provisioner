# Thème « Café » pour authentik

Thème de marque appliqué via le champ `branding_custom_css` d'un objet Brand
(le provisioner le pousse : voir `brands[].customCSS`).

## Contenu

| Fichier | Rôle |
| --- | --- |
| `cafe-custom.css` | CSS de marque **déployable** (couleurs + logo + fond embarqués en data-URI). C'est ce qui va dans `branding_custom_css`. |
| `cafe-background.jpg` | Image de fond source (FHD). |
| `logo.png` | Logo source. |
| `cafe-background.svg` | Ancien fond vectoriel (alternative légère). |

## Ce que fait le CSS

- **Couleurs** café : accent caramel + boutons `pf-m-primary` (coffee → roast).
- **Logo** : remplace `img.branding-logo` (le champ natif `branding_logo`
  n'accepte qu'un chemin de fichier servi par authentik, pas d'URL/data-URI).
- **Fond** : surcharge la variable native `--ak-global--background-image`
  (authentik 2026.x rend le fond sur `body::before` à partir de cette variable).

## Notes / limites (authentik 2026.5)

- `branding_logo` / `branding_default_flow_background` n'acceptent qu'un **chemin
  de fichier** servi par authentik (validateur : pas de `:`, donc ni URL ni
  data-URI). L'API n'accepte pas non plus l'upload (JSON only). D'où le passage
  du logo et du fond par le CSS.
- Pour un rendu 100 % natif (et un CSS léger), il faut monter ces fichiers dans
  le volume `/media/` d'authentik puis renseigner les champs avec leur chemin.
- **`sidebar_left`** (formulaire sur le côté) et **`show_source_labels`** sont
  des réglages natifs (Flow / Identification stage), pas du CSS.

## Régénérer le CSS après avoir changé une image

```sh
python3 - <<'PY'
import base64, re
css = open("cafe-custom.css").read()
def datauri(path, mime):
    return "data:%s;base64,%s" % (mime, base64.b64encode(open(path,"rb").read()).decode())
css = re.sub(r'(--cafe-flow-bg:\s*url\(")[^"]*("\))',
            lambda m: m.group(1)+datauri("cafe-background.jpg","image/jpeg")+m.group(2), css, 1)
css = re.sub(r'(content:\s*url\(")data:image/png[^"]*("\))',
            lambda m: m.group(1)+datauri("logo.png","image/png")+m.group(2), css, 1)
open("cafe-custom.css","w").write(css)
PY
```
