export interface EventType { id: string; label: string; icon: string; name: string; amount: number; startAge: number; years: number; taxable: boolean }

export const incomeTypes: EventType[] = [
  { id: 'social_security', label: 'Social Security', icon: 'shield', name: 'Social Security', amount: 36000, startAge: 67, years: 0, taxable: true },
  { id: 'pension', label: 'Pension', icon: 'umbrella', name: 'Pension', amount: 24000, startAge: 65, years: 0, taxable: true },
  { id: 'part_time', label: 'Part-time work', icon: 'coffee', name: 'Part-time work', amount: 30000, startAge: 50, years: 10, taxable: true },
  { id: 'rental', label: 'Rental income', icon: 'house', name: 'Rental income', amount: 18000, startAge: 55, years: 0, taxable: true },
  { id: 'other', label: 'Other income', icon: 'piggy', name: 'Other income', amount: 10000, startAge: 60, years: 0, taxable: false },
]
export const spendingTypes: EventType[] = [
  { id: 'health', label: 'Health care', icon: 'heart', name: 'Health care', amount: 12000, startAge: 55, years: 0, taxable: false },
  { id: 'travel', label: 'Travel', icon: 'palm', name: 'Travel', amount: 10000, startAge: 60, years: 15, taxable: false },
  { id: 'home', label: 'Home project', icon: 'house', name: 'Home project', amount: 30000, startAge: 50, years: 1, taxable: false },
  { id: 'education', label: 'Education', icon: 'trend', name: 'Education', amount: 25000, startAge: 45, years: 4, taxable: false },
  { id: 'other', label: 'Other spending', icon: 'debt', name: 'Other spending', amount: 5000, startAge: 60, years: 0, taxable: false },
]
export const iconFor = (kind: string, type: string): string =>
  [...incomeTypes, ...spendingTypes].find((t) => t.id === type && (kind === 'income' ? incomeTypes : spendingTypes).includes(t))?.icon ?? (kind === 'income' ? 'piggy' : 'debt')
