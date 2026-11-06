# AliceKit 2.6 — графика

Верхняя панель: настоящая фотография Станции Макс YNDX-0008, с сохранением фактуры, кнопок и пропорций. Источник указан в NOTICE. Снимок используется как отдельный слой; фактические цвета24диодов рисуются поверх кольца. Низ фотографии исчезает маской CSS. Редактор отдельно показывает черновик, пока пользователь не применил его. Фото не сгенерировано.

Логотип: сохранена прежняя фиолетовая объёмная эмблема. Шрифт Manrope с кириллицей находится в приложении и не запрашивается из интернета, лицензия SIL OFL1.1.

Новые изображения получены встроенным imagegen. Финальные проектные файлы:
- panel/web/launch-star.png — прозрачная объёмная звезда для вступительной анимации.
- panel/web/fabric-charcoal.jpg — материал акустической ткани для WebGL-модели.
- panel/web/station-max-top.jpg — фотография верхней панели, исключена из MIT-лицензии проекта.

## Prompt: launch star

Use case: product-mockup / standalone UI launch animation asset. The attached image is a STYLE AND MATERIAL REFERENCE only: match its glossy pearlescent white ceramic with saturated violet inner reflections, gentle lilac iridescence and expensive polished 3D industrial finish. Generate ONE single isolated three-dimensional four-point sculptural star, not an icon sheet, not flat vector, not a sparkle outline. It is a substantial smooth inflated machined object,4long elegantly curved tapered points, beveled edges and genuine thickness, viewed three-quarter from slightly above, rotated18degrees clockwise. Beautiful physically plausible studio lighting, precise specular highlights, violet self-reflections, subtle subsurface pearl, fine dimensional ambient occlusion. Entire star fully in frame with generous transparent margins, clean alpha cutout. No background, no square tile, no typography, no letters, no particles, no extra stars, no speaker or house. This will fly through3Dtransforms on a mobile app loading screen before resolving into the supplied emblem, so silhouette must be bold, dimension convincing at100px. Keep restrained Yandex-smart-home-inspired purple-white visual language; high resolution production raster with real transparent alpha.

## Prompt: acoustic fabric

Use case: photorealistic material texture for a real-time3Dproduct replica, not an illustration. The reference is an actual Yandex Station Max photograph; use ONLY the charcoal acoustic fabric around the outside rim as the material reference. Produce one seamless tileable square albedo/base-color map of that tightly woven dark charcoal acoustic speaker fabric. Orthographic macro photograph, perfectly even diffuse illumination, realistic interlaced heather charcoal-black yarn fibers, dense fine three-dimensional woven detail, consistent physical scale across the entire tile. Dark but with visible fibers and natural microvariation, neutral charcoal RGB roughly45-75, no purple tint. No device, no controls, no ring, no typography, no logo, no perspective, no gradients, no vignette, no borders, no shadow direction. Edges must repeat seamlessly horizontally and vertically. This asset is a production material for a faithful station product visualization, not a design mockup. Opaque image.


## TV banner 2.7

Separate 16:9 launcher artwork, generated with the built-in imagegen using the approved purple emblem as reference. Final assets: panel/web/tv-banner.jpg and panel/{controller,voice}/res/drawable-nodpi/alicekit_tv_banner.jpg. Phone launcher retains its square emblem. Resizing to 960×540 preserves the full generated composition.

Prompt: Create a finished Android TV launcher banner for AliceKit, exactly wide 16:9 landscape composition. Preserve the reference glossy white open ring surrounding house roof, violet inner glow, pearlescent ceramic material, and lavender brand identity. Emblem as a physical sculptural object on the left, no rounded-square tile. On the right wordmark exactly AliceKit with small TV underneath. Spacious safe margins, central 80% area. Pale lavender photographic studio backdrop, restrained violet caustic lighting, matte floor, soft realistic shadows. No other text, no symbols, no station image, no UI mockup or border. Entire image is banner artwork.
