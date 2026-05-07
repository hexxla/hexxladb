---
name: go-interface-design
description: Validates Go interface design and abstractions
---

# Go Interface Design Skill

In Go, interfaces are defined by the consumer, not the implementer. Small, focused interfaces are the foundation of testable, decoupled code. A large interface is a sign of over-coupling — prefer composition.

<checklist>
1. [ ] Interfaces are small — ideally 1–3 methods (Reader, Writer, Closer pattern)
2. [ ] Interfaces defined in the package that **uses** them, not the package that implements them
3. [ ] Interface names describe behaviour: `Reader`, `OrderRepository`, `PaymentProcessor`
4. [ ] No implementation details in interface method signatures (no DB types, no HTTP types)
5. [ ] Large interfaces decomposed into smaller composable ones
6. [ ] `any` / `interface{}` used only when genuinely type-agnostic
7. [ ] No unnecessary interface abstraction for types with a single implementation
8. [ ] Interface changes are backward compatible
</checklist>

<examples>
<example>BAD: `type OrderRepository interface { FindByID(...); FindAll(...); Save(...); Delete(...); Update(...); Count() }` — too large, hard to mock</example>
<example>GOOD: Split into `OrderReader`, `OrderWriter` — compose where both needed</example>
<example>BAD: Interface method returns `*sql.Rows` — leaks infrastructure into core</example>
<example>GOOD: Interface method returns domain type `[]Order`</example>
</examples>
