package main

// inspectorSignatures includes inherited selectors, with subclass overrides winning.
func inspectorSignatures(class *ClassDef) map[string][]string {
	signatures := make(map[string][]string)
	for current := class; current != nil; current = current.Superclass {
		for name, method := range current.Methods {
			if _, exists := signatures[name]; exists {
				continue
			} else {
				signatures[name] = append([]string{}, method.Params...)
			}
		}
	}
	return signatures
}
