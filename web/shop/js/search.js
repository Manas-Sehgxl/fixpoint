function fold(text) {
  return text.normalize("NFKD").replace(/\p{M}/gu, "").replace(/\s+/g, " ").trim();
}

export function searchProducts(products, query) {
  const words = fold(String(query ?? "")).split(" ").filter(Boolean);
  if (words.length === 0) return products;
  return products.filter((product) => {
    const haystack = fold(`${product.name} ${product.notes}`);
    return words.every((word) => haystack.includes(word));
  });
}
