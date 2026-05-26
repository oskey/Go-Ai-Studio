import * as React from "react"
import { Check, ChevronsUpDown } from "lucide-react"
import { cn } from "@/lib/utils"
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from "@/components/ui/command"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"

export interface ComboboxProps<T> {
  items: T[]
  value?: string | number
  onChange: (value: string | number) => void
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  renderItem: (item: T) => React.ReactNode
  getItemValue: (item: T) => string | number
  getItemLabel: (item: T) => string
}

export function Combobox<T>({
  items,
  value,
  onChange,
  placeholder = "请选择...",
  searchPlaceholder = "搜索...",
  emptyText = "未找到结果",
  renderItem,
  getItemValue,
  getItemLabel,
}: ComboboxProps<T>) {
  const [open, setOpen] = React.useState(false)
  const safeItems = Array.isArray(items) ? items : []

  const selectedItem = safeItems.find((item) => getItemValue(item) === value)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          role="combobox"
          aria-expanded={open}
          className="w-full flex items-center justify-between rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {selectedItem ? renderItem(selectedItem) : <span className="text-muted-foreground">{placeholder}</span>}
          <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-[--radix-popover-trigger-width] p-0" align="start">
        <Command
          filter={(itemValue, search) => {
            if (!search) return 1
            return itemValue.toLowerCase().includes(search.toLowerCase()) ? 1 : 0
          }}
        >
          <CommandInput placeholder={searchPlaceholder} />
          <CommandList>
            <CommandEmpty>{emptyText}</CommandEmpty>
            <CommandGroup>
              {safeItems.map((item) => (
                <CommandItem
                  key={getItemValue(item)}
                  value={getItemLabel(item)} // Use label for searching
                  onSelect={() => {
                    onChange(getItemValue(item))
                    setOpen(false)
                  }}
                >
                  <Check
                    className={cn(
                      "mr-2 h-4 w-4",
                      value === getItemValue(item) ? "opacity-100" : "opacity-0"
                    )}
                  />
                  {renderItem(item)}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
