// The menu. Prices are in whole rupees, per pack.
export const PRODUCTS = [
  {
    id: "masala-chai",
    name: "Masala Chai",
    price: 249,
    pack: "250 g pouch",
    notes: "Assam CTC with cardamom, clove, cinnamon and black pepper.",
    art: { vessel: "glass", liquid: "#B8763E", top: "#E2B98A", panel: "#F6B915" },
  },
  {
    id: "kadak-assam",
    name: "Kadak Assam",
    price: 250,
    pack: "250 g pouch",
    notes: "Strong, malty CTC leaf that stands up to plenty of milk.",
    art: { vessel: "kulhad", liquid: "#8A4E24", top: "#C89466", panel: "#D42A66" },
  },
  {
    id: "elaichi-chai",
    name: "Elaichi Chai",
    price: 180,
    pack: "200 g pouch",
    notes: "Assam leaf with crushed green cardamom pods.",
    art: { vessel: "glass", liquid: "#C99263", top: "#EBD0AE", panel: "#7FC6A4" },
  },
  {
    id: "adrak-chai",
    name: "Adrak Chai",
    price: 160,
    pack: "200 g pouch",
    notes: "Black tea with dried ginger for a sharp, warming cup.",
    art: { vessel: "kulhad", liquid: "#A65F2C", top: "#D7A877", panel: "#F28C28" },
  },
  {
    id: "kashmiri-kahwa",
    name: "Kashmiri Kahwa",
    price: 320,
    pack: "100 g tin",
    notes: "Green tea with saffron, cinnamon and almond slivers.",
    art: { vessel: "cup", liquid: "#E0A526", top: "#F3CF6A", panel: "#5BA4D9" },
  },
  {
    id: "filter-kaapi",
    name: "Filter Kaapi",
    price: 200,
    pack: "250 g pouch",
    notes: "Coffee and chicory, 80:20, ground for a steel filter.",
    art: { vessel: "tumbler", liquid: "#6B3B1F", top: "#C9A27E", panel: "#F6B915" },
  },
  {
    id: "nimbu-pudina",
    name: "Nimbu Pudina",
    price: 120,
    pack: "150 g pouch",
    notes: "Black tea with lemon peel and mint. Good over ice.",
    art: { vessel: "tall", liquid: "#C9902F", top: "#E8C46A", panel: "#9BD34F" },
  },
  {
    id: "tulsi-green",
    name: "Tulsi Green",
    price: 220,
    pack: "100 g tin",
    notes: "Green tea with holy basil leaves.",
    art: { vessel: "cup", liquid: "#A9B640", top: "#D5DC8A", panel: "#D42A66" },
  },
];

const byId = new Map(PRODUCTS.map((product) => [product.id, product]));

export function findProduct(id) {
  return byId.get(id) ?? null;
}
