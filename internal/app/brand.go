package app

import "github.com/egoist/mygo/ui"

// brandSVG 是侧栏顶部那个标：应用图标的小尺寸版（图形取自 design/icons/
// papero-icon-flat-small.svg，按 16 格重画了三处）。
//
// 用 Image 而不是 Icon：Icon 会把整张图刷成一种颜色，而这张标靠橙色的
// 书签、深色的圆点和蓝色的一条线区分彼此，刷成一色就只剩个白框。
//
// 和设计稿的三处差别，都是为了 16px 还能认出是「一页纸」：
//   - viewBox 裁到图形的实际范围（原稿四周留白 512 格，缩到 16px 纸只占一半）；
//   - 页面描边 10 → 26（原稿 32px 的细边，16px 下会消失）；
//   - 灰线 D8D8D6 → CFCFCC（原色在白底上几乎看不见）。
const brandSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="83 55 350 362" width="350" height="362">
  <path d="M208 116 C206 88 188 70 170 68 M208 116 C212 90 232 74 250 78" fill="none" stroke="#EA6A1F" stroke-width="26" stroke-linecap="round"/>
  <rect x="96" y="104" width="248" height="300" rx="44" fill="#FFFFFF" stroke="#8C8676" stroke-width="26"/>
  <path d="M340 194 L420 228 L340 264 Z" fill="#EA6A1F"/>
  <circle cx="288" cy="192" r="38" fill="#1F2937"/>
  <rect x="148" y="306" width="150" height="28" rx="14" fill="#CFCFCC"/>
  <rect x="148" y="356" width="104" height="30" rx="15" fill="#2563EB"/>
</svg>`

// iconBrand 是解析好的品牌标，包级只解一次。
var iconBrand = ui.MustParseSVG([]byte(brandSVG))
